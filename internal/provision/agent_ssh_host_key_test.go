package provision

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

type fakeAgentSSHHostKeyQueries struct {
	mu       sync.Mutex
	servers  map[uuid.UUID]db.GetServerForUpdateRow
	rows     map[string]db.GetServerAgentSSHHostKeyRow
	claims   int
	claimErr error
}

func newFakeAgentSSHHostKeyQueries(serverIDs ...uuid.UUID) *fakeAgentSSHHostKeyQueries {
	q := &fakeAgentSSHHostKeyQueries{servers: make(map[uuid.UUID]db.GetServerForUpdateRow), rows: make(map[string]db.GetServerAgentSSHHostKeyRow)}
	for _, serverID := range serverIDs {
		q.servers[serverID] = db.GetServerForUpdateRow{ID: serverID, TenantID: uuid.New(), SiteID: uuid.New()}
	}
	return q
}

func (q *fakeAgentSSHHostKeyQueries) GetServerForUpdate(_ context.Context, id uuid.UUID) (db.GetServerForUpdateRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	server, ok := q.servers[id]
	if !ok {
		return db.GetServerForUpdateRow{}, errors.New("server unavailable")
	}
	return server, nil
}

func hostKeyRowKey(serverID uuid.UUID, host string, port int32) string {
	return fmt.Sprintf("%s/%s/%d", serverID, host, port)
}

func (q *fakeAgentSSHHostKeyQueries) ClaimServerAgentSSHHostKey(_ context.Context, arg db.ClaimServerAgentSSHHostKeyParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	if q.claimErr != nil {
		return 0, q.claimErr
	}
	key := hostKeyRowKey(arg.ServerID, arg.Host, arg.SshPort)
	if _, exists := q.rows[key]; exists {
		return 0, nil
	}
	q.rows[key] = db.GetServerAgentSSHHostKeyRow{ServerID: arg.ServerID, TenantID: arg.TenantID, Host: arg.Host, SshPort: arg.SshPort, Fingerprint: arg.Fingerprint}
	return 1, nil
}

func (q *fakeAgentSSHHostKeyQueries) GetServerAgentSSHHostKey(_ context.Context, arg db.GetServerAgentSSHHostKeyParams) (db.GetServerAgentSSHHostKeyRow, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	row, ok := q.rows[hostKeyRowKey(arg.ServerID, arg.Host, arg.SshPort)]
	if !ok {
		return db.GetServerAgentSSHHostKeyRow{}, errors.New("trust row not found")
	}
	return row, nil
}

type fakeAgentSSHHostKeyChecker struct{ deny authz.Permission }

func (c fakeAgentSSHHostKeyChecker) Require(permission authz.Permission, _ authz.Resource) error {
	if c.deny == permission {
		return errors.New("permission denied")
	}
	return nil
}

func permitAgentSSHHostKey(_ context.Context, _ db.GetServerForUpdateRow) (agentSSHHostKeyPermissionChecker, error) {
	return fakeAgentSSHHostKeyChecker{}, nil
}

func TestPersistAgentSSHHostKeyClaimsFirstKeyAndRejectsChangedKey(t *testing.T) {
	serverID := uuid.New()
	q := newFakeAgentSSHHostKeyQueries(serverID)
	actor := authz.Actor{UserID: uuid.New()}
	keyA := "SHA256:" + strings.Repeat("A", 43)
	keyB := "SHA256:" + strings.Repeat("B", 43)
	call := func(host string, port uint16, key string) error {
		return persistAgentSSHHostKeyInTransaction(context.Background(), q, permitAgentSSHHostKey, actor, serverID, host, port, key)
	}
	if err := call("192.0.2.10", 22, keyA); err != nil {
		t.Fatalf("first key should be stored: %v", err)
	}
	if err := call("192.0.2.10", 22, keyA); err != nil {
		t.Fatalf("matching key should be accepted: %v", err)
	}
	if err := call("192.0.2.10", 22, keyB); !errors.Is(err, ErrAgentSSHHostKeyMismatch) {
		t.Fatalf("changed key error = %v, want mismatch", err)
	}
	if err := call("192.0.2.11", 22, keyB); err != nil {
		t.Fatalf("separate host should get separate trust: %v", err)
	}
	if err := call("192.0.2.10", 2222, keyB); err != nil {
		t.Fatalf("separate SSH port should get separate trust: %v", err)
	}
	otherServer := uuid.New()
	q.servers[otherServer] = db.GetServerForUpdateRow{ID: otherServer, TenantID: uuid.New(), SiteID: uuid.New()}
	if err := persistAgentSSHHostKeyInTransaction(context.Background(), q, permitAgentSSHHostKey, actor, otherServer, "192.0.2.10", 22, keyB); err != nil {
		t.Fatalf("separate server should get separate trust: %v", err)
	}
	if got := len(q.rows); got != 4 {
		t.Fatalf("trust records = %d, want distinct server, host and port scopes", got)
	}
}

func TestPersistAgentSSHHostKeyStorageFailureDoesNotClaimTrust(t *testing.T) {
	serverID := uuid.New()
	q := newFakeAgentSSHHostKeyQueries(serverID)
	q.claimErr = errors.New("database unavailable")
	err := persistAgentSSHHostKeyInTransaction(context.Background(), q, permitAgentSSHHostKey, authz.Actor{UserID: uuid.New()}, serverID, "192.0.2.10", 22, "SHA256:"+strings.Repeat("A", 43))
	if err == nil {
		t.Fatal("expected trust-store failure")
	}
	if len(q.rows) != 0 {
		t.Fatal("trust row was created after store failure")
	}
}

func TestPersistAgentSSHHostKeyRechecksBothPermissionsBeforeClaim(t *testing.T) {
	for _, permission := range []authz.Permission{authz.ServersManage, authz.ServersConfigSecrets} {
		t.Run(string(permission), func(t *testing.T) {
			serverID := uuid.New()
			q := newFakeAgentSSHHostKeyQueries(serverID)
			loader := func(context.Context, db.GetServerForUpdateRow) (agentSSHHostKeyPermissionChecker, error) {
				return fakeAgentSSHHostKeyChecker{deny: permission}, nil
			}
			err := persistAgentSSHHostKeyInTransaction(context.Background(), q, loader, authz.Actor{UserID: uuid.New()}, serverID, "192.0.2.10", 22, "SHA256:"+strings.Repeat("A", 43))
			if err == nil {
				t.Fatal("expected scoped permission denial")
			}
			if q.claims != 0 || len(q.rows) != 0 {
				t.Fatal("host-key trust was written after permission denial")
			}
		})
	}
}

func TestPersistAgentSSHHostKeyConcurrentFirstContactsKeepSingleWinner(t *testing.T) {
	serverID := uuid.New()
	q := newFakeAgentSSHHostKeyQueries(serverID)
	actor := authz.Actor{UserID: uuid.New()}
	var wg sync.WaitGroup
	results := make([]error, 32)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fingerprint := "SHA256:" + strings.Repeat(string(rune('A'+i%2)), 43)
			results[i] = persistAgentSSHHostKeyInTransaction(context.Background(), q, permitAgentSSHHostKey, actor, serverID, "192.0.2.10", 22, fingerprint)
		}(i)
	}
	wg.Wait()
	winner := q.rows[hostKeyRowKey(serverID, "192.0.2.10", 22)].Fingerprint
	for i, err := range results {
		input := "SHA256:" + strings.Repeat(string(rune('A'+i%2)), 43)
		if input == winner && err != nil {
			t.Errorf("same-key contender %d failed: %v", i, err)
		}
		if input != winner && !errors.Is(err, ErrAgentSSHHostKeyMismatch) {
			t.Errorf("competing-key contender %d error = %v, want mismatch", i, err)
		}
	}
}
