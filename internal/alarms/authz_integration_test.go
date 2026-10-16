//go:build integration

package alarms_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/store"
)

func TestSingleAlarmOperationsHonorServerAndCameraGroupScopes(t *testing.T) {
	c := newCorrEnv(t)
	ctx := context.Background()
	svc := &alarms.Service{Store: c.env.Store}

	res := c.correlate(c.cam, "event", nil, t0, "person")
	alarmID := res.AlarmID

	otherCam := c.env.Cameras["frigate-c01/muelle"]

	var serverOp, groupOp, otherServerOp authz.Actor
	var groupID uuid.UUID
	err := c.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		createUser := func(username string) (authz.Actor, error) {
			var uid uuid.UUID
			err := tx.QueryRow(ctx, `
INSERT INTO users (tenant_id, username, display_name, status)
VALUES ($1, $2, $2, 'active') RETURNING id`, c.cam.TenantID, username).Scan(&uid)
			if err != nil {
				return authz.Actor{}, err
			}
			tID := c.cam.TenantID
			return authz.Actor{UserID: uid, Username: username, TenantID: &tID}, nil
		}
		grant := func(userID uuid.UUID, perm authz.Permission, scopeType authz.ScopeType, scopeID uuid.UUID) error {
			_, err := tx.Exec(ctx, `
INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
VALUES ($1, 'user', $2, $3, 'allow', $4, $5)`,
				c.cam.TenantID, userID, string(perm), string(scopeType), scopeID)
			return err
		}

		var err error
		if serverOp, err = createUser("server-scope-op"); err != nil {
			return err
		}
		if err := grant(serverOp.UserID, authz.AlarmsView, authz.ScopeServer, c.cam.ServerID); err != nil {
			return err
		}
		if err := grant(serverOp.UserID, authz.AlarmsManage, authz.ScopeServer, c.cam.ServerID); err != nil {
			return err
		}

		if groupOp, err = createUser("group-scope-op"); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
INSERT INTO camera_groups (tenant_id, name, description)
VALUES ($1, 'Grupo Plaza', '') RETURNING id`, c.cam.TenantID).Scan(&groupID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO camera_group_members (group_id, camera_id, tenant_id)
VALUES ($1, $2, $3)`, groupID, c.cam.ID, c.cam.TenantID); err != nil {
			return err
		}
		if err := grant(groupOp.UserID, authz.AlarmsView, authz.ScopeCameraGroup, groupID); err != nil {
			return err
		}
		if err := grant(groupOp.UserID, authz.AlarmsManage, authz.ScopeCameraGroup, groupID); err != nil {
			return err
		}

		if otherServerOp, err = createUser("other-server-op"); err != nil {
			return err
		}
		if err := grant(otherServerOp.UserID, authz.AlarmsView, authz.ScopeServer, otherCam.ServerID); err != nil {
			return err
		}
		if err := grant(otherServerOp.UserID, authz.AlarmsManage, authz.ScopeServer, otherCam.ServerID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed actors and grants: %v", err)
	}

	// Server-scoped operator can view and operate on the alarm.
	if _, err := svc.Get(ctx, serverOp, alarmID); err != nil {
		t.Fatalf("serverOp Get: %v", err)
	}
	if _, err := svc.Acknowledge(ctx, serverOp, alarmID, "ack by server op"); err != nil {
		t.Fatalf("serverOp Acknowledge: %v", err)
	}
	if _, err := svc.AddComment(ctx, serverOp, alarmID, "comment by server op"); err != nil {
		t.Fatalf("serverOp AddComment: %v", err)
	}
	if _, err := svc.ListTransitions(ctx, serverOp, alarmID); err != nil {
		t.Fatalf("serverOp ListTransitions: %v", err)
	}
	if _, err := svc.AssignableUsers(ctx, serverOp, alarmID); err != nil {
		t.Fatalf("serverOp AssignableUsers: %v", err)
	}

	// Camera-group-scoped operator can view, assign, investigate, resolve, and close the alarm.
	if _, err := svc.Get(ctx, groupOp, alarmID); err != nil {
		t.Fatalf("groupOp Get: %v", err)
	}
	if _, err := svc.Assign(ctx, groupOp, alarmID, serverOp.UserID); err != nil {
		t.Fatalf("groupOp Assign: %v", err)
	}
	if _, err := svc.Investigate(ctx, groupOp, alarmID, "investigating by group op"); err != nil {
		t.Fatalf("groupOp Investigate: %v", err)
	}
	if _, err := svc.Resolve(ctx, groupOp, alarmID, "resolved by group op"); err != nil {
		t.Fatalf("groupOp Resolve: %v", err)
	}
	if _, err := svc.Close(ctx, groupOp, alarmID, "closed by group op"); err != nil {
		t.Fatalf("groupOp Close: %v", err)
	}

	// Operator scoped to a different server is still forbidden.
	if _, err := svc.Get(ctx, otherServerOp, alarmID); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("otherServerOp Get err = %v, want ErrForbidden", err)
	}
}
