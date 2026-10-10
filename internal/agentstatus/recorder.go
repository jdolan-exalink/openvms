// Package agentstatus persists what an enrolled edge agent reports in its heartbeat: when it
// was last seen, its version and its operational status.
package agentstatus

import (
	"context"
	"errors"
	"fmt"

	"github.com/jdolan-exalink/openvms/internal/agentca"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// MaxVersionLen bounds the version string an agent can make the API store.
const MaxVersionLen = 64

// Statuses are the operational statuses an agent can report (the agent_heartbeats check).
var statuses = map[string]bool{"unknown": true, "online": true, "degraded": true, "offline": true, "maintenance": true}

// ErrInvalid marks a heartbeat whose content is not storable (status outside the known set,
// version too long). It is the agent's mistake, not a database failure.
var ErrInvalid = errors.New("agentstatus: invalid heartbeat")

// Heartbeat is what the agent reported.
type Heartbeat struct {
	Version string
	Status  string
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
	if !statuses[hb.Status] {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, hb.Status)
	}
	if len(hb.Version) > MaxVersionLen {
		return fmt.Errorf("%w: version longer than %d bytes", ErrInvalid, MaxVersionLen)
	}
	return r.Store.Tx(ctx, store.TenantScope{TenantID: id.TenantID}, func(q *db.Queries) error {
		return q.UpsertAgentHeartbeat(ctx, db.UpsertAgentHeartbeatParams{
			ServerID: id.ServerID, TenantID: id.TenantID, Version: hb.Version, Status: hb.Status,
		})
	})
}
