package agentca

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"sync"
	"testing"
	"time"

	"github.com/jdolan-exalink/openvms/internal/secrets"
)

type fakeRepo struct {
	mu    sync.Mutex
	ca    *CARecord
	certs map[string]Certificate
	// beforeInsert runs once just before the CA insert, to simulate a concurrent winner.
	beforeInsert func(r *fakeRepo)
	getCalls     int
}

func (r *fakeRepo) GetCA(context.Context) (*CARecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls++
	if r.ca == nil {
		return nil, ErrNoCA
	}
	c := *r.ca
	return &c, nil
}

func (r *fakeRepo) InsertCAIfAbsent(_ context.Context, rec CARecord) (bool, error) {
	if h := r.beforeInsert; h != nil {
		r.beforeInsert = nil
		h(r)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ca != nil {
		return false, nil
	}
	r.ca = &rec
	return true, nil
}

func (r *fakeRepo) InsertCertificate(_ context.Context, c Certificate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.certs == nil {
		r.certs = map[string]Certificate{}
	}
	r.certs[c.Serial] = c
	return nil
}

func (r *fakeRepo) GetCertificate(_ context.Context, serial string) (Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.certs[serial]
	if !ok {
		return Certificate{}, ErrCertificateNotFound
	}
	return c, nil
}

func newService(t *testing.T, repo Repository) *Service {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	sealer, err := secrets.NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Repo: repo, Sealer: sealer, Now: func() time.Time { return testNow }}
}

func TestLoadOrCreateCASealsKeyAndIsStable(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(t, repo)
	first, err := svc.LoadOrCreateCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	der, _ := first.MarshalKey()
	if bytes.Contains(repo.ca.KeySealed, der) || bytes.Contains(repo.ca.KeySealed, der[len(der)-32:]) {
		t.Fatal("CA key is stored in the clear")
	}
	second, err := svc.LoadOrCreateCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cert.Equal(first.Cert) || !second.Key.Equal(first.Key) {
		t.Fatal("second call returned a different CA")
	}
}

func TestLoadOrCreateCAAdoptsConcurrentWinner(t *testing.T) {
	repo := &fakeRepo{}
	winner := newService(t, repo)
	loser := &Service{Repo: repo, Sealer: winner.Sealer, Now: winner.Now}
	var winnerCA *CA
	repo.beforeInsert = func(r *fakeRepo) {
		var err error
		winnerCA, err = winner.LoadOrCreateCA(context.Background())
		if err != nil {
			t.Error(err)
		}
	}
	got, err := loser.LoadOrCreateCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if winnerCA == nil || !got.Cert.Equal(winnerCA.Cert) {
		t.Fatal("loser did not adopt the CA that won the race")
	}
}

func TestLoadOrCreateCAFailsOnWrongMasterKey(t *testing.T) {
	repo := &fakeRepo{}
	if _, err := newService(t, repo).LoadOrCreateCA(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := newService(t, repo).LoadOrCreateCA(context.Background()); err == nil {
		t.Fatal("opened the CA key with a different master key")
	}
}

func TestIssueSignsAndRecordsCertificate(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(t, repo)
	id := testIdentity()
	res, err := svc.Issue(context.Background(), newCSR(t, p256(t), &x509.CertificateRequest{}), id)
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := repo.certs[res.Issued.Serial]
	if !ok {
		t.Fatal("issued certificate was not recorded")
	}
	if rec.TenantID != id.TenantID || rec.ServerID != id.ServerID || rec.Fingerprint != res.Issued.Fingerprint || !rec.NotAfter.Equal(res.Issued.NotAfter) {
		t.Fatalf("recorded = %+v", rec)
	}
	ca, _ := ParseCA(res.CAPEM, mustKey(t, svc))
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	leaf := parseLeaf(t, res.Issued.CertPEM)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: testNow, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("returned CA bundle does not verify the leaf: %v", err)
	}
}

func TestSignUsesTheGivenCAWithoutTouchingTheRepository(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(t, repo)
	ca, err := svc.LoadOrCreateCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	loads := repo.getCalls
	res, err := svc.Sign(ca, newCSR(t, p256(t), &x509.CertificateRequest{}), testIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if repo.getCalls != loads || len(repo.certs) != 0 {
		t.Fatalf("Sign touched the repository: GetCA calls %d -> %d, %d certificates recorded", loads, repo.getCalls, len(repo.certs))
	}
	if res.Record.Serial != res.Issued.Serial {
		t.Fatalf("record %+v does not describe the issued certificate", res.Record)
	}
}

func TestIssueCachesTheParsedCA(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(t, repo)
	csr := newCSR(t, p256(t), &x509.CertificateRequest{})
	if _, err := svc.Issue(context.Background(), csr, testIdentity()); err != nil {
		t.Fatal(err)
	}
	loads := repo.getCalls
	if _, err := svc.Issue(context.Background(), csr, testIdentity()); err != nil {
		t.Fatal(err)
	}
	if repo.getCalls != loads {
		t.Fatalf("second Issue read the CA from the repository again (%d -> %d GetCA calls)", loads, repo.getCalls)
	}
}

func TestIssueRejectsBadCSRWithoutRecording(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(t, repo)
	if _, err := svc.Issue(context.Background(), []byte("junk"), testIdentity()); err == nil {
		t.Fatal("accepted junk CSR")
	}
	if len(repo.certs) != 0 {
		t.Fatal("recorded a certificate for a rejected CSR")
	}
}

func mustKey(t *testing.T, svc *Service) []byte {
	t.Helper()
	ca, err := svc.LoadOrCreateCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	der, _ := ca.MarshalKey()
	return der
}
