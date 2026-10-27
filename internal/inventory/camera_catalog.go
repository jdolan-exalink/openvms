package inventory

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/camconfig"
	"github.com/jdolan-exalink/openvms/internal/store/db"
)

// modelLabelsTTL bounds how long a server's model labels are reused: reading them costs a
// full GET /api/config and the model rarely changes.
const modelLabelsTTL = 5 * time.Minute

type cachedLabels struct {
	labels  []string
	expires time.Time
}

var modelLabelCache sync.Map // uuid.UUID -> cachedLabels

// ServerModelLabels returns the object labels the Frigate model of a server reports, for the
// object catalog. The actor needs servers.view on the server (an error otherwise). When the
// server or its labels cannot be read the result is nil, meaning unknown, never an error.
func (s *Service) ServerModelLabels(ctx context.Context, actor authz.Actor, serverID uuid.UUID) ([]string, error) {
	var srv db.GetServerRow
	err := s.tx(ctx, actor, func(q *db.Queries, c *access.Checker) error {
		var err error
		if srv, err = q.GetServer(ctx, serverID); err != nil {
			return notFoundOr(err)
		}
		return c.Require(authz.ServersView, access.Server(srv.TenantID, srv.SiteID, srv.ID))
	})
	if err != nil {
		return nil, err
	}
	if v, ok := modelLabelCache.Load(serverID); ok {
		if c := v.(cachedLabels); time.Now().Before(c.expires) {
			return c.labels, nil
		}
	}
	a, err := s.frigateAdapter(ctx, srv)
	if err != nil {
		return nil, nil //nolint:nilerr // an unreachable server means unknown labels
	}
	labels := camconfig.ModelLabels(ctx, a)
	if labels != nil {
		modelLabelCache.Store(serverID, cachedLabels{labels: labels, expires: time.Now().Add(modelLabelsTTL)})
	}
	return labels, nil
}
