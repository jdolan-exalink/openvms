//go:build integration

package inventory_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/store/db"
	"github.com/jdolan-exalink/openvms/internal/testutil/demofix"
)

type recordingBlobs struct {
	mu      sync.Mutex
	deleted []string
	fail    bool
}

func (b *recordingBlobs) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = append(b.deleted, key)
	if b.fail {
		return errors.New("storage down")
	}
	return nil
}

// seedServerData inserts one row per table that hangs off a camera or its server.
func seedServerData(t *testing.T, env *demofix.Env, cam db.GetCameraRow, tag string) {
	t.Helper()
	ctx := context.Background()
	evID, readID, groupID, viewID, ruleID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	layout := `{"columns": 2, "cells": [{"camera_id": "` + cam.ID.String() + `", "quality": "sub"}, null]}`
	err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
		stmts := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO events (id, tenant_id, site_id, server_id, camera_id, remote_id, severity, start_time, thumb_key, preview_key)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, 'alert', now(), 'thumb/' || $6, 'preview/' || $6)`,
				[]any{evID, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, tag}},
			{`INSERT INTO lpr_reads (id, tenant_id, site_id, server_id, camera_id, remote_event_id, plate, plate_normalized, seen_at)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, 'AB123CD', 'AB123CD', now())`,
				[]any{readID, cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, tag}},
			{`INSERT INTO alarms (tenant_id, site_id, camera_id, event_id, source, status) VALUES ($1, $2, $3, $4, 'event', 'open')`,
				[]any{cam.TenantID, cam.SiteID, cam.ID, evID}},
			{`INSERT INTO exports (tenant_id, site_id, server_id, camera_id, requested_by, name, start_time, end_time)
			  VALUES ($1, $2, $3, $4, $5, 'exp', now(), now())`,
				[]any{cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, env.Admin.UserID}},
			{`INSERT INTO clip_watermark_jobs (tenant_id, site_id, server_id, camera_id, lpr_read_id, remote_event_id, requested_by, watermark_text, output_key)
			  VALUES ($1, $2, $3, $4, $5, 'r-' || $6, $7, 'wm', 'clip/' || $6)`,
				[]any{cam.TenantID, cam.SiteID, cam.ServerID, cam.ID, readID, tag, env.Admin.UserID}},
			{`INSERT INTO object_snapshots (server_id, tenant_id, remote_object_id) VALUES ($1, $2, 'obj-' || $3)`,
				[]any{cam.ServerID, cam.TenantID, tag}},
			{`INSERT INTO event_sync_state (server_id, tenant_id) VALUES ($1, $2)`, []any{cam.ServerID, cam.TenantID}},
			{`INSERT INTO camera_outages (camera_id, tenant_id) VALUES ($1, $2)`, []any{cam.ID, cam.TenantID}},
			{`INSERT INTO camera_groups (id, tenant_id, name) VALUES ($1, $2, 'g-' || $3)`, []any{groupID, cam.TenantID, tag}},
			{`INSERT INTO camera_group_members (group_id, camera_id, tenant_id) VALUES ($1, $2, $3)`, []any{groupID, cam.ID, cam.TenantID}},
			{`INSERT INTO views (id, tenant_id, owner_id, name, layout) VALUES ($1, $2, $3, 'v-' || $4, $5::jsonb)`,
				[]any{viewID, cam.TenantID, env.Admin.UserID, tag, layout}},
			{`INSERT INTO rules (id, tenant_id, name, trigger_type) VALUES ($1, $2, 'r-' || $3, 'event')`, []any{ruleID, cam.TenantID, tag}},
			{`INSERT INTO rule_firings (rule_id, resource_id, tenant_id, fired_at) VALUES ($1, $2, $3, now())`, []any{ruleID, cam.ID, cam.TenantID}},
			{`INSERT INTO permission_grants (tenant_id, subject_type, subject_id, permission, effect, scope_type, scope_id)
			  VALUES ($1, 'user', $2, 'cameras.view', 'allow', 'server', $3)`, []any{cam.TenantID, env.Admin.UserID, cam.ServerID}},
		}
		for _, s := range stmts {
			if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed %s: %v", tag, err)
	}
}

func count(t *testing.T, env *demofix.Env, sql string, args ...any) int {
	t.Helper()
	var n int
	// The pool runs as the RLS-bound application role, so count with the all-tenants scope.
	err := env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), sql, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func TestDeleteServerHardDeletesEverything(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	blobs := &recordingBlobs{}
	env.Svc.Blobs = blobs
	doomedCam := env.Cameras["frigate-h01/acceso_norte"]
	keptCam := env.Cameras["frigate-c01/muelle"]
	doomed, kept := doomedCam.ServerID, keptCam.ServerID
	seedServerData(t, env, doomedCam, "doomed")
	seedServerData(t, env, keptCam, "kept")

	perServer := map[string]string{
		"events":               `SELECT count(*) FROM events WHERE server_id = $1`,
		"lpr_reads":            `SELECT count(*) FROM lpr_reads WHERE server_id = $1`,
		"alarms":               `SELECT count(*) FROM alarms a JOIN cameras c ON c.id = a.camera_id WHERE c.server_id = $1`,
		"exports":              `SELECT count(*) FROM exports WHERE server_id = $1`,
		"clip_watermark_jobs":  `SELECT count(*) FROM clip_watermark_jobs WHERE server_id = $1`,
		"object_snapshots":     `SELECT count(*) FROM object_snapshots WHERE server_id = $1`,
		"event_sync_state":     `SELECT count(*) FROM event_sync_state WHERE server_id = $1`,
		"camera_outages":       `SELECT count(*) FROM camera_outages o JOIN cameras c ON c.id = o.camera_id WHERE c.server_id = $1`,
		"camera_group_members": `SELECT count(*) FROM camera_group_members m JOIN cameras c ON c.id = m.camera_id WHERE c.server_id = $1`,
		"rule_firings":         `SELECT count(*) FROM rule_firings f JOIN cameras c ON c.id = f.resource_id WHERE c.server_id = $1`,
		"cameras":              `SELECT count(*) FROM cameras WHERE server_id = $1`,
		"server grants":        `SELECT count(*) FROM permission_grants WHERE scope_type = 'server' AND scope_id = $1`,
	}
	for name, q := range perServer {
		if count(t, env, q, doomed) == 0 || count(t, env, q, kept) == 0 {
			t.Fatalf("seed for %s is missing (doomed=%d kept=%d)", name, count(t, env, q, doomed), count(t, env, q, kept))
		}
	}

	t.Run("operator without servers.manage is forbidden and nothing changes", func(t *testing.T) {
		err := env.Svc.DeleteServer(ctx, env.Actor(t, "operador"), doomed)
		if !errors.Is(err, access.ErrForbidden) && !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("DeleteServer = %v, want forbidden", err)
		}
		if n := count(t, env, `SELECT count(*) FROM frigate_servers WHERE id = $1`, doomed); n != 1 {
			t.Fatalf("server rows = %d after refused delete", n)
		}
	})

	if err := env.Svc.DeleteServer(ctx, env.Admin, doomed); err != nil {
		t.Fatal(err)
	}

	t.Run("every row of the server is gone", func(t *testing.T) {
		if n := count(t, env, `SELECT count(*) FROM frigate_servers WHERE id = $1`, doomed); n != 0 {
			t.Errorf("frigate_servers rows = %d, want 0 (hard delete)", n)
		}
		if n := count(t, env, `SELECT count(*) FROM cameras WHERE server_id = $1`, doomed); n != 0 {
			t.Errorf("cameras rows = %d, want 0", n)
		}
		for name, q := range perServer {
			if n := count(t, env, q, doomed); n != 0 {
				t.Errorf("%s rows for the deleted server = %d, want 0", name, n)
			}
		}
		if n := count(t, env, `SELECT count(*) FROM permission_grants WHERE scope_type = 'camera' AND scope_id IN (SELECT id FROM cameras WHERE server_id = $1)`, doomed); n != 0 {
			t.Errorf("camera grants = %d, want 0", n)
		}
	})

	t.Run("another server is untouched", func(t *testing.T) {
		for name, q := range perServer {
			if n := count(t, env, q, kept); n == 0 {
				t.Errorf("%s rows of the other server were deleted", name)
			}
		}
		if n := count(t, env, `SELECT count(*) FROM frigate_servers WHERE id = $1 AND deleted_at IS NULL`, kept); n != 1 {
			t.Errorf("other server rows = %d, want 1", n)
		}
	})

	t.Run("views drop the deleted cameras but keep their shape", func(t *testing.T) {
		if n := count(t, env, `SELECT count(*) FROM views WHERE name = 'v-doomed' AND jsonb_array_length(layout->'cells') = 2 AND layout->'cells'->0 = 'null'::jsonb`); n != 1 {
			t.Errorf("doomed view layout not cleared")
		}
		if n := count(t, env, `SELECT count(*) FROM views WHERE name = 'v-kept' AND layout->'cells'->0->>'camera_id' = $1`, keptCam.ID.String()); n != 1 {
			t.Errorf("other view layout was modified")
		}
	})

	t.Run("audit log is kept and records the counts", func(t *testing.T) {
		var events, cameras int
		err := env.Pool.QueryRow(ctx, `SELECT (details->>'events')::int, (details->>'cameras')::int FROM audit_log WHERE action = 'SERVER_REMOVED' AND target_id = $1`, doomed).Scan(&events, &cameras)
		if err != nil {
			t.Fatalf("SERVER_REMOVED audit entry: %v", err)
		}
		if events != 1 || cameras != 4 {
			t.Errorf("audit counts events=%d cameras=%d, want 1 and 4", events, cameras)
		}
		if n := count(t, env, `SELECT count(*) FROM audit_log WHERE action = 'SERVER_ADDED' AND target_id = $1`, doomed); n == 0 {
			t.Error("earlier audit rows of the server were lost")
		}
	})

	t.Run("stored objects are deleted after commit", func(t *testing.T) {
		want := []string{"clip/doomed", "preview/doomed", "thumb/doomed"}
		got := slices.Clone(blobs.deleted)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("deleted objects = %v, want %v", got, want)
		}
	})

	t.Run("unknown server is not found", func(t *testing.T) {
		if err := env.Svc.DeleteServer(ctx, env.Admin, doomed); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("second delete = %v, want not found", err)
		}
	})
}

func TestDeleteServerSurvivesObjectStoreFailure(t *testing.T) {
	env := demofix.Setup(t)
	env.Svc.Blobs = &recordingBlobs{fail: true}
	cam := env.Cameras["frigate-h01/acceso_norte"]
	seedServerData(t, env, cam, "x")
	if err := env.Svc.DeleteServer(context.Background(), env.Admin, cam.ServerID); err != nil {
		t.Fatalf("DeleteServer must not fail on object store errors: %v", err)
	}
	if n := count(t, env, `SELECT count(*) FROM frigate_servers WHERE id = $1`, cam.ServerID); n != 0 {
		t.Errorf("server rows = %d", n)
	}
}

// Rules keep their remaining ids; a rule whose only camera/server filter disappears is
// disabled instead of silently widening to "all". Notifications tied to the server go away.
func TestDeleteServerCleansRulesAndNotifications(t *testing.T) {
	env := demofix.Setup(t)
	ctx := context.Background()
	doomedCam := env.Cameras["frigate-h01/acceso_norte"]
	keptCam := env.Cameras["frigate-c01/muelle"]
	doomed, kept := doomedCam.ServerID, keptCam.ServerID
	tenant := doomedCam.TenantID

	rule := func(name, conditions string) uuid.UUID {
		id := uuid.New()
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO rules (id, tenant_id, name, trigger_type, conditions, enabled) VALUES ($1, $2, $3, 'event', $4::jsonb, true)`,
				id, tenant, name, conditions)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	q := func(ids ...uuid.UUID) string {
		s := "["
		for i, id := range ids {
			if i > 0 {
				s += ","
			}
			s += `"` + id.String() + `"`
		}
		return s + "]"
	}
	partial := rule("partial", `{"camera_ids": `+q(doomedCam.ID, keptCam.ID)+`, "labels": ["person"]}`)
	onlyCam := rule("only-camera", `{"camera_ids": `+q(doomedCam.ID)+`}`)
	partialSrv := rule("partial-server", `{"server_ids": `+q(doomed, kept)+`}`)
	onlySrv := rule("only-server", `{"server_ids": `+q(doomed)+`}`)
	untouched := rule("all-cameras", `{"labels": ["car"]}`)

	notif := func(server, camera *uuid.UUID) uuid.UUID {
		id := uuid.New()
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO notifications (id, tenant_id, title, body, server_id, camera_id) VALUES ($1, $2, 't', 'b', $3, $4)`,
				id, tenant, server, camera)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	byServer, byCamera := notif(&doomed, nil), notif(nil, &doomedCam.ID)
	otherCamera, unlinked := notif(nil, &keptCam.ID), notif(nil, nil)

	if err := env.Svc.DeleteServer(ctx, env.Admin, doomed); err != nil {
		t.Fatal(err)
	}

	type state struct {
		enabled bool
		cams    string
		srvs    string
	}
	get := func(id uuid.UUID) state {
		var s state
		err := env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT enabled, coalesce(conditions->'camera_ids', 'null'::jsonb)::text, coalesce(conditions->'server_ids', 'null'::jsonb)::text FROM rules WHERE id = $1`, id).Scan(&s.enabled, &s.cams, &s.srvs)
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := get(partial); !s.enabled || s.cams != `["`+keptCam.ID.String()+`"]` {
		t.Errorf("partial rule = %+v, want enabled with only the kept camera", s)
	}
	if s := get(onlyCam); s.enabled || s.cams != "[]" {
		t.Errorf("only-camera rule = %+v, want disabled with an empty filter", s)
	}
	if s := get(partialSrv); !s.enabled || s.srvs != `["`+kept.String()+`"]` {
		t.Errorf("partial-server rule = %+v, want enabled with only the kept server", s)
	}
	if s := get(onlySrv); s.enabled || s.srvs != "[]" {
		t.Errorf("only-server rule = %+v, want disabled", s)
	}
	if s := get(untouched); !s.enabled || s.cams != "null" || s.srvs != "null" {
		t.Errorf("unfiltered rule = %+v, want untouched", s)
	}

	for id, want := range map[uuid.UUID]int{byServer: 0, byCamera: 0, otherCamera: 1, unlinked: 1} {
		if n := count(t, env, `SELECT count(*) FROM notifications WHERE id = $1`, id); n != want {
			t.Errorf("notification %s rows = %d, want %d", id, n, want)
		}
	}

	var notifications, updated, disabled int
	err := env.Pool.QueryRow(ctx, `SELECT (details->>'notifications')::int, (details->>'rules_updated')::int, (details->>'rules_disabled')::int FROM audit_log WHERE action = 'SERVER_REMOVED' AND target_id = $1`, doomed).Scan(&notifications, &updated, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	if notifications != 2 || updated != 4 || disabled != 2 {
		t.Errorf("audit notifications=%d rules_updated=%d rules_disabled=%d, want 2, 4, 2", notifications, updated, disabled)
	}
}
