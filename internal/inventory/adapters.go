package inventory

import (
	"bytes"
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/frigate"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// Adapters keeps one connected adapter per Frigate server so the health poller, the
// event syncer and the media gateway reuse Frigate sessions instead of logging in on
// every request. An entry is replaced when the stored connection changes.
type Adapters struct {
	Svc *Service

	mu      sync.Mutex
	entries map[uuid.UUID]cachedAdapter
}

type cachedAdapter struct {
	sealed   []byte
	baseURL  string
	authMode string
	tlsSkip  bool
	adapter  frigate.Adapter
}

func NewAdapters(svc *Service) *Adapters {
	return &Adapters{Svc: svc, entries: map[uuid.UUID]cachedAdapter{}}
}

// Get returns a connected adapter for srv, connecting (and detecting the version) if needed.
func (p *Adapters) Get(ctx context.Context, srv db.FrigateServer) (frigate.Adapter, error) {
	p.mu.Lock()
	c, ok := p.entries[srv.ID]
	p.mu.Unlock()
	if ok && bytes.Equal(c.sealed, srv.PasswordSealed) && c.baseURL == srv.BaseUrl && c.authMode == srv.AuthMode && c.tlsSkip == srv.TlsSkipVerify {
		return c.adapter, nil
	}
	info, err := p.Svc.connInfo(srv)
	if err != nil {
		return nil, err
	}
	a, err := p.Svc.Connect(ctx, info)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.entries[srv.ID] = cachedAdapter{sealed: srv.PasswordSealed, baseURL: srv.BaseUrl, authMode: srv.AuthMode, tlsSkip: srv.TlsSkipVerify, adapter: a}
	p.mu.Unlock()
	return a, nil
}

// Forget drops the cached adapter so the next Get reconnects and re-detects the version.
func (p *Adapters) Forget(id uuid.UUID) {
	p.mu.Lock()
	delete(p.entries, id)
	p.mu.Unlock()
}
