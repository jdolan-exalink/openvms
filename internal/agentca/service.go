package agentca

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/secrets"
)

// ErrNoCA is returned by Repository.GetCA when the CA has not been created yet.
var ErrNoCA = errors.New("agentca: no CA")

// ErrCertificateNotFound is returned by Repository.GetCertificate when no certificate has
// the serial, in the same vocabulary as ErrNoCA.
var ErrCertificateNotFound = errors.New("agentca: certificate not found")

// ErrCertificateRevoked is returned when a certificate could not be activated because it is
// revoked, for example a renewal replaced it while its first call was in flight, or because its
// row no longer exists.
var ErrCertificateRevoked = errors.New("agentca: certificate is revoked")

// ErrNotRenewable is returned when the certificate presented for a renewal cannot be renewed:
// it is unknown for that server and tenant, revoked (including superseded by a successor that
// was already used), or has a used successor.
var ErrNotRenewable = errors.New("agentca: certificate cannot be renewed")

// ErrRenewalTooEarly is returned when an agent asks to renew a certificate that has not yet
// passed the minimum age for renewal (half of its lifetime).
var ErrRenewalTooEarly = errors.New("agentca: certificate is too new to renew")

// caKeyAAD binds the sealed CA key to its purpose, so a sealed value copied from another
// table or column does not open as the CA key.
var caKeyAAD = []byte("openvms/agent-ca/key/v1")

// CARecord is the persisted CA: certificate in the clear, key sealed.
type CARecord struct {
	CertPEM   string
	KeySealed []byte
}

// Certificate is the record kept for every issued agent certificate.
type Certificate struct {
	Serial      string
	TenantID    uuid.UUID
	ServerID    uuid.UUID
	Fingerprint string
	NotBefore   time.Time
	NotAfter    time.Time
	RevokedAt   *time.Time
	// ParentSerial is the certificate this one renewed; empty for an enrollment.
	ParentSerial string
	// FirstUsedAt is when the certificate first authenticated a call, nil until then. Using a
	// certificate revokes the server's other certificates (see PgRepo.ActivateCertificate).
	FirstUsedAt *time.Time
}

// Repository is the persistence port of the service.
type Repository interface {
	// GetCA returns ErrNoCA when no CA exists.
	GetCA(ctx context.Context) (*CARecord, error)
	// InsertCAIfAbsent stores rec only when no CA exists and reports whether it did.
	InsertCAIfAbsent(ctx context.Context, rec CARecord) (bool, error)
	InsertCertificate(ctx context.Context, c Certificate) error
	// GetCertificate returns the record for a serial, or ErrCertificateNotFound.
	GetCertificate(ctx context.Context, serial string) (Certificate, error)
}

// Service owns the CA lifecycle and certificate issuance.
type Service struct {
	Repo     Repository
	Sealer   *secrets.Sealer
	Now      func() time.Time // defaults to time.Now
	Validity time.Duration    // leaf lifetime; defaults to DefaultLeafValidity

	mu sync.Mutex
	ca *CA // parsed CA, kept after the first successful load
}

// Issuance is a signed certificate and the CA certificate the agent must trust.
type Issuance struct {
	Issued *Issued
	CAPEM  []byte
	// Record is the row to persist for the certificate; Sign leaves that to the caller.
	Record Certificate
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// LoadOrCreateCA returns the installation CA, generating and persisting it on first use.
// Concurrent first callers each generate a CA but only one insert wins; the others
// discard theirs and use the stored one. The parsed CA is cached after the first success,
// so issuance does not reread and reopen the sealed key on every request; the CA is
// immutable once stored (rotation is out of scope).
func (s *Service) LoadOrCreateCA(ctx context.Context) (*CA, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ca != nil {
		return s.ca, nil
	}
	rec, err := s.Repo.GetCA(ctx)
	if errors.Is(err, ErrNoCA) {
		rec, err = s.createCA(ctx)
	}
	if err != nil {
		return nil, err
	}
	keyDER, err := s.Sealer.Open(rec.KeySealed, caKeyAAD)
	if err != nil {
		return nil, fmt.Errorf("agentca: open CA key: %w", err)
	}
	ca, err := ParseCA([]byte(rec.CertPEM), keyDER)
	if err != nil {
		return nil, err
	}
	s.ca = ca
	return ca, nil
}

func (s *Service) createCA(ctx context.Context) (*CARecord, error) {
	ca, err := GenerateCA(s.now())
	if err != nil {
		return nil, err
	}
	keyDER, err := ca.MarshalKey()
	if err != nil {
		return nil, err
	}
	sealed, err := s.Sealer.Seal(keyDER, caKeyAAD)
	if err != nil {
		return nil, fmt.Errorf("agentca: seal CA key: %w", err)
	}
	if _, err := s.Repo.InsertCAIfAbsent(ctx, CARecord{CertPEM: string(ca.CertPEM), KeySealed: sealed}); err != nil {
		return nil, err
	}
	// Re-read whether or not this insert won, so every caller converges on the stored CA.
	return s.Repo.GetCA(ctx)
}

// Sign signs a CSR for id with ca and does not record the certificate. It takes the CA
// explicitly (see LoadOrCreateCA) and has no context: it never reaches the repository, so a
// caller can use it inside a database transaction without a second connection. A caller that
// must record the certificate together with other writes persists Issuance.Record (see
// RecordCertificate) and releases the certificate only after that commit.
func (s *Service) Sign(ca *CA, csrPEM []byte, id Identity) (*Issuance, error) {
	validity := s.Validity
	if validity == 0 {
		validity = DefaultLeafValidity
	}
	issued, err := ca.SignCSR(csrPEM, id, s.now(), validity)
	if err != nil {
		return nil, err
	}
	return &Issuance{Issued: issued, CAPEM: ca.CertPEM, Record: Certificate{
		Serial:      issued.Serial,
		TenantID:    id.TenantID,
		ServerID:    id.ServerID,
		Fingerprint: issued.Fingerprint,
		NotBefore:   issued.NotBefore,
		NotAfter:    issued.NotAfter,
	}}, nil
}

// Issue signs a CSR for id and records the certificate. It returns the CA certificate
// alongside so the agent can pin the trust root it will use for the API.
//
// Errors are returned exactly as their source produced them, with no context added by Issue:
// a CA load failure, ErrInvalidCSR or
// another signing error, or the repository's error from InsertCertificate. Callers can match
// them with errors.Is.
func (s *Service) Issue(ctx context.Context, csrPEM []byte, id Identity) (*Issuance, error) {
	ca, err := s.LoadOrCreateCA(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.Sign(ca, csrPEM, id)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.InsertCertificate(ctx, res.Record); err != nil {
		return nil, err
	}
	return res, nil
}
