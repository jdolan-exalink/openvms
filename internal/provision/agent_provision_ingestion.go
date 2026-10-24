package provision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

// LPRIngestion is how far central got collecting a server agent's plate readings: what the worker
// job records after every pull. Known is false until the first pull of that server.
type LPRIngestion struct {
	Known         bool       `json:"known"`
	LastSeq       int64      `json:"last_seq"`
	IngestedTotal int64      `json:"ingested_total"`
	SkippedTotal  int64      `json:"skipped_total"`
	AgentLost     int64      `json:"agent_lost"`
	AgentDepth    int        `json:"agent_depth"`
	LastPullAt    *time.Time `json:"last_pull_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// GetProvisionLPRIngestion reads the pull state of a server's agent queue. The caller must be able to
// manage the server, like for the agent's registry.
func (s *Service) GetProvisionLPRIngestion(ctx context.Context, actor authz.Actor, serverID uuid.UUID) (LPRIngestion, error) {
	if err := s.authorizeServerManage(ctx, actor, serverID); err != nil {
		return LPRIngestion{}, err
	}
	var out LPRIngestion
	err := s.Store.TxRaw(ctx, store.ScopeFor(actor), func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
SELECT last_seq, ingested_total, skipped_total, agent_lost, agent_depth, last_pull_at, last_error
FROM server_agent_event_cursors WHERE server_id = $1`, serverID).
			Scan(&out.LastSeq, &out.IngestedTotal, &out.SkippedTotal, &out.AgentLost, &out.AgentDepth, &out.LastPullAt, &out.LastError)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out.Known = err == nil
		return err
	})
	if err != nil {
		return LPRIngestion{}, fmt.Errorf("read agent ingestion state: %w", err)
	}
	return out, nil
}
