// Package agentstatus persists what an enrolled edge agent reports in its heartbeat: when it
// was last seen, its version and its operational status.
package agentstatus

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// MaxVersionLen bounds the version string an agent can make the API store.
const MaxVersionLen = 64

// Statuses is the vocabulary of agent statuses: the single source of truth. The recorder, the
// control layer's mapping from the wire enum and the tests use it. The agent_heartbeats CHECK
// constraint (migration 00044) and the OpenAPI AgentStatus enum repeat it because SQL and YAML
// must; TestVocabularyMatchesTheMigrationAndTheContract fails when either drifts.
var Statuses = []string{Unknown, Online, Degraded, Offline, Maintenance}

// The statuses, as stored.
const (
	Unknown     = "unknown"
	Online      = "online"
	Degraded    = "degraded"
	Offline     = "offline"
	Maintenance = "maintenance"
)

// Valid reports whether s is in Statuses.
func Valid(s string) bool { return slices.Contains(Statuses, s) }

// ErrInvalid marks a heartbeat whose content is not storable (status outside the vocabulary,
// version too long). It is the agent's mistake, permanent for that content, not a database
// failure, so it must not be retried or reported as unavailability.
var ErrInvalid = errors.New("agentstatus: invalid heartbeat")

// ErrUnknownServer means the server (or its tenant pairing) does not exist any more: the
// foreign key refused the row. Also permanent, not an outage.
var ErrUnknownServer = errors.New("agentstatus: unknown server")

// Heartbeat is what the agent reported.
type Heartbeat struct {
	Version string
	Status  string
}

// Validate is the one place the content limits are checked; Recorder calls it, so a caller that
// reaches the database has been through it.
func (h Heartbeat) Validate() error {
	if !Valid(h.Status) {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, h.Status)
	}
	if len(h.Version) > MaxVersionLen {
		return fmt.Errorf("%w: version longer than %d bytes", ErrInvalid, MaxVersionLen)
	}
	return nil
}

// Recorder writes heartbeats. Each heartbeat is one upsert in a transaction scoped to the
// agent's own tenant, so a mismatched identity could not touch another tenant's rows even
// without the composite foreign key that also refuses it.
//
// Cost: one small indexed write per heartbeat per server (every 10 seconds per agent by
// default). That is fine for the fleet sizes this targets; if it ever matters, skip the write
// when last_seen_at is recent and the version and status are unchanged.
type Recorder struct {
	Store *store.Store
}

// RecordHeartbeat stores hb as the latest state of id's server. last_seen_at is the database
// clock, never the agent's.
func (r *Recorder) RecordHeartbeat(ctx context.Context, id agentca.Identity, hb Heartbeat) error {
	if err := hb.Validate(); err != nil {
		return err
	}
	err := r.Store.Tx(ctx, store.TenantScope{TenantID: id.TenantID}, func(q *db.Queries) error {
		return q.UpsertAgentHeartbeat(ctx, db.UpsertAgentHeartbeatParams{
			ServerID: id.ServerID, TenantID: id.TenantID, Version: hb.Version, Status: hb.Status,
		})
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("%w: %v", ErrUnknownServer, err)
	}
	return err
}
