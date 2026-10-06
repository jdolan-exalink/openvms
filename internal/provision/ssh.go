package provision

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Conn is one SSH session to the host being installed.
type Conn interface {
	Run(ctx context.Context, cmd string) (string, error)
	WriteFile(ctx context.Context, path string, mode os.FileMode, data []byte) error
	Close() error
}

type sshConn struct {
	client *ssh.Client
	secret string
}

func dialSSH(ctx context.Context, host, user, password, expectedKey string, onKey func(fingerprint string)) (Conn, error) {
	return dialSSHAtPort(ctx, host, 22, user, password, expectedKey, onKey)
}

func dialSSHAtPort(ctx context.Context, host string, port uint16, user, password, expectedKey string, onKey func(fingerprint string)) (Conn, error) {
	return dialSSHAtPortWithConfig(ctx, host, port, password, clientConfig(user, password, expectedKey, onKey))
}

func dialSSHAtPortWithHostKeyTrust(ctx context.Context, host string, port uint16, user, password string, trust func(context.Context, string) error) (Conn, error) {
	return dialSSHAtPortWithConfig(ctx, host, port, password, clientConfigWithHostKeyTrust(user, password, func(fingerprint string) error {
		return trust(ctx, fingerprint)
	}))
}

func dialSSHAtPortWithConfig(ctx context.Context, host string, port uint16, password string, cfg *ssh.ClientConfig) (Conn, error) {
	if port == 0 || ctx.Err() != nil {
		return nil, errors.New("invalid SSH target")
	}
	address := net.JoinHostPort(host, strconv.Itoa(int(port)))
	d := net.Dialer{Timeout: 20 * time.Second}
	netConn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	handshakeDeadline := time.Now().Add(20 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(handshakeDeadline) {
		handshakeDeadline = deadline
	}
	if err := netConn.SetDeadline(handshakeDeadline); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = netConn.Close() })
	c, chans, reqs, err := ssh.NewClientConn(netConn, address, cfg)
	stopClose()
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := netConn.SetDeadline(time.Time{}); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = netConn.Close()
		return nil, err
	}
	return &sshConn{client: ssh.NewClient(c, chans, reqs), secret: password}, nil
}

func dialAgentSSH(ctx context.Context, host string, port uint16, user, password, expectedKey string) (AgentInstallConn, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || ip.String() != host || port == 0 || user != "root" || password == "" || !validFingerprint(expectedKey) || ctx.Err() != nil {
		return nil, errors.New("invalid pinned SSH target")
	}
	conn, err := dialSSHAtPort(ctx, host, port, user, password, expectedKey, nil)
	if err != nil {
		return nil, err
	}
	installConn, ok := conn.(AgentInstallConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("SSH connection does not support SFTP")
	}
	return installConn, nil
}

func dialAgentSSHWithHostKeyTrust(ctx context.Context, host string, port uint16, user, password string, trust func(context.Context, string) error) (AgentInstallConn, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || ip.String() != host || port == 0 || user != "root" || password == "" || trust == nil || ctx.Err() != nil {
		return nil, errors.New("invalid SSH host-key trust target")
	}
	conn, err := dialSSHAtPortWithHostKeyTrust(ctx, host, port, user, password, trust)
	if err != nil {
		return nil, err
	}
	installConn, ok := conn.(AgentInstallConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("SSH connection does not support SFTP")
	}
	return installConn, nil
}

func clientConfig(user, password, expectedKey string, onKey func(fingerprint string)) *ssh.ClientConfig {
	return clientConfigWithHostKeyTrust(user, password, func(fingerprint string) error {
		if expectedKey != "" && fingerprint != expectedKey {
			return fmt.Errorf("SSH host key fingerprint mismatch")
		}
		if expectedKey == "" && onKey == nil {
			return fmt.Errorf("SSH host key trust is required")
		}
		if onKey != nil {
			onKey(fingerprint)
		}
		return nil
	})
}

func clientConfigWithHostKeyTrust(user, password string, trust func(fingerprint string) error) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				if len(questions) != 1 {
					return nil, fmt.Errorf("unexpected ssh prompt")
				}
				return []string{password}, nil
			}),
		},
		// Host-key trust is resolved before SSH user authentication, so a failed or
		// mismatched first-contact store cannot send the password.
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint := ssh.FingerprintSHA256(key)
			if trust == nil {
				return fmt.Errorf("SSH host key trust is required")
			}
			return trust(fingerprint)
		},
		// Preserve OpenSSH host-key preference order: x/crypto can otherwise select an
		// RSA key before the server's preferred key and establish a different trust anchor.
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256,
			ssh.KeyAlgoECDSA384,
			ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoRSASHA256,
		},
		Timeout: 20 * time.Second,
	}
}

func (c *sshConn) Close() error { c.secret = ""; return c.client.Close() }

func (c *sshConn) Run(ctx context.Context, cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	// buf keeps both streams for the caller; errBuf keeps stderr alone because the failure
	// reason is usually there and stdout written later would push it out of the error tail.
	var buf, errBuf syncBuffer
	session.Stdout = &buf
	session.Stderr = io.MultiWriter(&buf, &errBuf)
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return Scrub(buf.String(), c.secret), ctx.Err()
	case err := <-errCh:
		out := Scrub(buf.String(), c.secret)
		if err != nil {
			reason := Scrub(errBuf.String(), c.secret)
			if strings.TrimSpace(reason) == "" {
				reason = out
			}
			return out, fmt.Errorf("%w: %s", err, tail(reason, 400))
		}
		return out, nil
	}
}

func (c *sshConn) WriteFile(ctx context.Context, path string, mode os.FileMode, data []byte) error {
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(data)
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s && chmod %o %s", quote(dirOf(path)), quote(path), mode.Perm(), quote(path))
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// WriteSFTPFile transfers one file as an SSH SFTP payload. New files are created exclusively,
// chmodded before data is written, then populated; secrets are never placed in an exec command.
func (c *sshConn) WriteSFTPFile(ctx context.Context, remotePath string, mode os.FileMode, data []byte) error {
	if !path.IsAbs(remotePath) || path.Clean(remotePath) != remotePath {
		return errors.New("invalid SFTP destination")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	stopSSHClose := context.AfterFunc(ctx, func() { _ = c.client.Close() })
	defer stopSSHClose()
	client, err := sftp.NewClient(c.client)
	if err != nil {
		return errors.New("open SFTP channel")
	}
	stopClose := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer func() {
		stopClose()
		_ = client.Close()
	}()
	if err := client.MkdirAll(path.Dir(remotePath)); err != nil {
		return errors.New("create SFTP directory")
	}
	file, err := client.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return errors.New("create SFTP file")
	}
	if err := file.Close(); err != nil {
		return errors.New("initialize SFTP file")
	}
	if err := client.Chmod(remotePath, mode.Perm()); err != nil {
		return errors.New("secure SFTP file mode")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err = client.OpenFile(remotePath, os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return errors.New("open SFTP payload")
	}
	_, copyErr := io.Copy(file, bytes.NewReader(data))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return errors.New("write SFTP payload")
	}
	return ctx.Err()
}

// syncBuffer collects stdout and stderr together. x/crypto copies each stream in
// its own goroutine, so a plain bytes.Buffer would interleave corrupted writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			if i == 0 {
				return "/"
			}
			return path[:i]
		}
	}
	return "."
}
