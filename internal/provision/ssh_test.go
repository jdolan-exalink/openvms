package provision

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startSSHServer serves one connection with the given host keys. exec, when set,
// answers "exec" requests and returns the exit status.
func startSSHServer(t *testing.T, keys []ssh.Signer, exec func(cmd string, ch ssh.Channel) uint32) string {
	t.Helper()
	server := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return nil, nil },
	}
	for _, k := range keys {
		server.AddHostKey(k)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		nc, err := ln.Accept()
		if err != nil {
			return
		}
		defer nc.Close()
		conn, chans, reqs, err := ssh.NewServerConn(nc, server)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(reqs)
		for nch := range chans {
			if exec == nil || nch.ChannelType() != "session" {
				_ = nch.Reject(ssh.Prohibited, "test server")
				continue
			}
			ch, chReqs, err := nch.Accept()
			if err != nil {
				return
			}
			go func() {
				defer ch.Close()
				for req := range chReqs {
					if req.Type != "exec" || len(req.Payload) < 4 {
						_ = req.Reply(false, nil)
						continue
					}
					_ = req.Reply(true, nil)
					status := exec(string(req.Payload[4:]), ch)
					payload := make([]byte, 4)
					binary.BigEndian.PutUint32(payload, status)
					_, _ = ch.SendRequest("exit-status", false, payload)
					return
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func newSigner(t *testing.T, key any) ssh.Signer {
	t.Helper()
	s, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newEd25519Signer(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return newSigner(t, priv)
}

func dialTest(t *testing.T, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	t.Helper()
	nc, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// A stock Debian/Ubuntu host offers RSA, ECDSA and ED25519 host keys. Operators
// copy the fingerprint OpenSSH shows them, which is the ED25519 key. The client
// must negotiate that key instead of x/crypto's RSA-first default.
func TestClientConfigVerifiesOpenSSHPreferredHostKey(t *testing.T) {
	edSigner := newEd25519Signer(t)
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	addr := startSSHServer(t, []ssh.Signer{newSigner(t, rsaPriv), edSigner}, nil)

	want := ssh.FingerprintSHA256(edSigner.PublicKey())
	var seen string
	c, err := dialTest(t, addr, clientConfig("root", "pw", want, func(fp string) { seen = fp }))
	if err != nil {
		t.Fatalf("handshake with the ED25519 fingerprint failed: %v", err)
	}
	_ = c.Close()
	if seen != want {
		t.Fatalf("verified fingerprint = %q, want the ED25519 key", seen)
	}
}

// The failure reason is usually on stderr and can be buried under later stdout; the error
// must still carry it, and fall back to stdout when stderr is empty.
func TestRunErrorPrefersStderrTail(t *testing.T) {
	addr := startSSHServer(t, []ssh.Signer{newEd25519Signer(t)}, func(cmd string, ch ssh.Channel) uint32 {
		if cmd == "stdout-only" {
			_, _ = ch.Write([]byte("only stdout explains it\n"))
			return 2
		}
		_, _ = ch.Stderr().Write([]byte("Job for chrony.service failed\n"))
		for i := 0; i < 200; i++ {
			_, _ = ch.Write([]byte("progress line\n"))
		}
		return 1
	})
	client, err := dialTest(t, addr, clientConfig("root", "pw", "", nil))
	if err != nil {
		t.Fatal(err)
	}
	conn := &sshConn{client: client, secret: "pw"}
	defer conn.Close()

	if _, err := conn.Run(context.Background(), "bash install.sh ntp"); err == nil || !strings.Contains(err.Error(), "Job for chrony.service failed") {
		t.Fatalf("error must carry the stderr reason, got %v", err)
	}
	if _, err := conn.Run(context.Background(), "stdout-only"); err == nil || !strings.Contains(err.Error(), "only stdout explains it") {
		t.Fatalf("error must fall back to stdout when stderr is empty, got %v", err)
	}
}

// Installer failures are only diagnosable if both streams reach the operator intact.
func TestRunKeepsStdoutAndStderr(t *testing.T) {
	const lines = 200
	addr := startSSHServer(t, []ssh.Signer{newEd25519Signer(t)}, func(_ string, ch ssh.Channel) uint32 {
		for i := 0; i < lines; i++ {
			_, _ = ch.Write([]byte("out-line\n"))
			_, _ = ch.Stderr().Write([]byte("err-line\n"))
		}
		return 1
	})
	client, err := dialTest(t, addr, clientConfig("root", "pw", "", nil))
	if err != nil {
		t.Fatal(err)
	}
	conn := &sshConn{client: client, secret: "pw"}
	defer conn.Close()

	out, err := conn.Run(context.Background(), "bash install.sh ntp")
	if err == nil {
		t.Fatal("non-zero exit status must be an error")
	}
	if got := strings.Count(out, "out-line\n"); got != lines {
		t.Errorf("stdout lines = %d, want %d", got, lines)
	}
	if got := strings.Count(out, "err-line\n"); got != lines {
		t.Errorf("stderr lines = %d, want %d", got, lines)
	}
	if !strings.Contains(err.Error(), "err-line") {
		t.Errorf("error must carry the command output tail: %v", err)
	}
}
