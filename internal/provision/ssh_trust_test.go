package provision

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestClientConfigPersistsFirstHostKeyBeforePasswordAuthentication(t *testing.T) {
	key := newEd25519Signer(t)
	want := ssh.FingerprintSHA256(key.PublicKey())
	var mu sync.Mutex
	var events []string
	server := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "password")
		if len(events) != 2 || events[0] != "persist:"+want {
			return nil, errors.New("host key was not persisted before password authentication")
		}
		return nil, nil
	}}
	server.AddHostKey(key)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		serverConn, chans, reqs, err := ssh.NewServerConn(conn, server)
		if err == nil {
			go ssh.DiscardRequests(reqs)
			for channel := range chans {
				_ = channel.Reject(ssh.Prohibited, "test")
			}
			_ = serverConn.Close()
		}
		serverDone <- err
	}()
	clientConn, err := dialAgentSSHWithHostKeyTrust(context.Background(), "127.0.0.1", uint16(listener.Addr().(*net.TCPAddr).Port), "root", "pw", func(_ context.Context, fingerprint string) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, "persist:"+fingerprint)
		return nil
	})
	if err != nil {
		t.Fatalf("SSH handshake should succeed after key persistence: %v", err)
	}
	_ = clientConn.Close()
	if err := <-serverDone; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || events[0] != "persist:"+want || events[1] != "password" {
		t.Fatalf("callback order = %#v, want persist then password", events)
	}
}

func TestClientConfigRejectsFirstHostKeyWhenPersistenceFailsBeforePasswordAuth(t *testing.T) {
	key := newEd25519Signer(t)
	passwordCalls := 0
	server := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		passwordCalls++
		return nil, nil
	}}
	server.AddHostKey(key)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_, _, _, _ = ssh.NewServerConn(conn, server)
		}
	}()
	client, err := dialAgentSSHWithHostKeyTrust(context.Background(), "127.0.0.1", uint16(listener.Addr().(*net.TCPAddr).Port), "root", "pw", func(context.Context, string) error {
		return errors.New("trust store unavailable")
	})
	if client != nil {
		_ = client.Close()
	}
	if err == nil {
		t.Fatal("expected SSH host-key trust persistence failure")
	}
	<-serverDone
	if passwordCalls != 0 {
		t.Fatalf("password authentication was attempted %d times after trust persistence failure", passwordCalls)
	}
}
