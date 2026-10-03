package provision

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

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

func dialSSH(ctx context.Context, host, user, password string, onKey func(fingerprint string)) (Conn, error) {
	cfg := &ssh.ClientConfig{
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
		// First contact: the fingerprint is stored on the job so the operator can see it.
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if onKey != nil {
				onKey(ssh.FingerprintSHA256(key))
			}
			return nil
		},
		Timeout: 20 * time.Second,
	}
	d := net.Dialer{Timeout: 20 * time.Second}
	netConn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "22"))
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(netConn, net.JoinHostPort(host, "22"), cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	return &sshConn{client: ssh.NewClient(c, chans, reqs), secret: password}, nil
}

func (c *sshConn) Close() error { return c.client.Close() }

func (c *sshConn) Run(ctx context.Context, cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf
	errCh := make(chan error, 1)
	go func() { errCh <- session.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		return Scrub(buf.String(), c.secret), ctx.Err()
	case err := <-errCh:
		out := Scrub(buf.String(), c.secret)
		if err != nil {
			return out, fmt.Errorf("%w: %s", err, tail(out, 400))
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
