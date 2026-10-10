package agentca

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/secrets"
)

// ErrNoCA is returned by Repository.GetCA when the CA has not been created yet.
var ErrNoCA = errors.New("agentca: no CA")

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
}

// Repository is the persistence port of the service.
type Repository interface {
	// GetCA returns ErrNoCA when no CA exists.
	GetCA(ctx context.Context) (*CARecord, error)
	// InsertCAIfAbsent stores rec only when no CA exists and reports whether it did.
	InsertCAIfAbsent(ctx context.Context, rec CARecord) (bool, error)
	InsertCertificate(ctx context.Context, c Certificate) error
}

// Service owns the CA lifecycle and certificate issuance.
type Service struct {
	Repo     Repository
	Sealer   *secrets.Sealer
	Now      func() time.Time // defaults to time.Now
	Validity time.Duration    // leaf lifetime; defaults to DefaultLeafValidity
}

// Issuance is a signed certificate and the CA certificate the agent must trust.
type Issuance struct {
	Issued *Issued
	CAPEM  []byte
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// LoadOrCreateCA returns the installation CA, generating and persisting it on first use.
// Concurrent first callers each generate a CA but only one insert wins; the others
// discard theirs and use the stored one.
func (s *Service) LoadOrCreateCA(ctx context.Context) (*CA, error) {
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
	return ParseCA([]byte(rec.CertPEM), keyDER)
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

// Issue signs a CSR for id and records the certificate. It returns the CA certificate
// alongside so the agent can pin the trust root it will use for the API.
func (s *Service) Issue(ctx context.Context, csrPEM []byte, id Identity) (*Issuance, error) {
	ca, err := s.LoadOrCreateCA(ctx)
	if err != nil {
		return nil, err
	}
	validity := s.Validity
	if validity == 0 {
		validity = DefaultLeafValidity
	}
	issued, err := ca.SignCSR(csrPEM, id, s.now(), validity)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.InsertCertificate(ctx, Certificate{
		Serial:      issued.Serial,
		TenantID:    id.TenantID,
		ServerID:    id.ServerID,
		Fingerprint: issued.Fingerprint,
		NotBefore:   issued.NotBefore,
		NotAfter:    issued.NotAfter,
	}); err != nil {
		return nil, fmt.Errorf("agentca: record certificate: %w", err)
	}
	return &Issuance{Issued: issued, CAPEM: ca.CertPEM}, nil
}
