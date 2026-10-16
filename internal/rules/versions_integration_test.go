//go:build integration

package rules_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
)

type versionRow struct {
	Version   int
	Snapshot  map[string]any
	ChangedBy *uuid.UUID
}

func (f *fixture) versions(ruleID uuid.UUID) []versionRow {
	f.t.Helper()
	var out []versionRow
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(),
			`SELECT version, snapshot, changed_by FROM rule_versions WHERE rule_id = $1 ORDER BY version`, ruleID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v versionRow
			var raw []byte
			if err := rows.Scan(&v.Version, &raw, &v.ChangedBy); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &v.Snapshot); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// ruleManager returns a tenant user holding rules.manage and rules.read.
func (f *fixture) ruleManager() authz.Actor {
	f.t.Helper()
	ctx := context.Background()
	tenantID := f.env.Demo.TenantID
	actor, _, err := bootstrap.TenantUser(ctx, f.env.Store, tenantID, "gestor-reglas-"+uuid.NewString()[:8])
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range []authz.Permission{authz.RulesManage, authz.RulesRead} {
		if _, err := f.env.Svc.CreateGrant(ctx, f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p,
			Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	return actor
}

func TestRuleVersionsRecordedOnCreateAndUpdate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	actor := f.ruleManager()

	created, err := f.svc.CreateRule(ctx, actor, rules.CreateRuleRequest{
		Name: "v1", TriggerType: rules.TriggerEvent, Enabled: true,
		Conditions: rules.Conditions{Labels: []string{"person"}},
		Actions:    rules.Actions{NotifyInApp: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Version != 1 {
		t.Fatalf("created version = %d, want 1", created.Version)
	}

	newName := "v2"
	updated, err := f.svc.UpdateRule(ctx, actor, created.ID, rules.UpdateRuleRequest{ExpectedVersion: 1, Name: &newName})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Name != "v2" {
		t.Fatalf("updated = %+v, want version 2 name v2", updated)
	}

	// A stale version conflicts and writes nothing.
	_, err = f.svc.UpdateRule(ctx, actor, created.ID, rules.UpdateRuleRequest{ExpectedVersion: 1, Name: &newName})
	if !errors.Is(err, rules.ErrVersionConflict) {
		t.Fatalf("stale update err = %v, want ErrVersionConflict", err)
	}

	vs := f.versions(created.ID)
	if len(vs) != 2 {
		t.Fatalf("rule_versions rows = %d, want 2: %+v", len(vs), vs)
	}
	if vs[0].Version != 1 || vs[0].Snapshot["name"] != "v1" {
		t.Fatalf("version 1 snapshot = %+v", vs[0])
	}
	if vs[1].Version != 2 || vs[1].Snapshot["name"] != "v2" {
		t.Fatalf("version 2 snapshot = %+v", vs[1])
	}
	if vs[1].ChangedBy == nil || *vs[1].ChangedBy != actor.UserID {
		t.Fatalf("changed_by = %v, want %v", vs[1].ChangedBy, actor.UserID)
	}
}

func TestRuleUpdateWithoutExpectedVersionSkipsCheck(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	created, err := f.svc.CreateRule(ctx, f.ruleManager(), rules.CreateRuleRequest{
		Name: "x", TriggerType: rules.TriggerEvent, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	name := "y"
	updated, err := f.svc.UpdateRule(ctx, f.ruleManager(), created.ID, rules.UpdateRuleRequest{Name: &name})
	if err != nil {
		t.Fatalf("update without version: %v", err)
	}
	if updated.Version != created.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, created.Version+1)
	}
	_, err = f.svc.UpdateRule(ctx, f.ruleManager(), created.ID, rules.UpdateRuleRequest{ExpectedVersion: -1, Name: &name})
	var ve *rules.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("negative version err = %v, want ValidationError", err)
	}
}

// insertLegacyRule stores a rule directly, bypassing validation, to mimic rows
// written before ValidateConditions existed.
func (f *fixture) insertLegacyRule(conditions string) uuid.UUID {
	f.t.Helper()
	id := uuid.New()
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO rules (id, tenant_id, name, trigger_type, conditions, actions, enabled)
			 VALUES ($1, $2, 'legacy', 'event', $3::jsonb, '{}'::jsonb, true)`,
			id, f.env.Demo.TenantID, conditions)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func TestRuleUpdateNonConditionPatchOnLegacyInvalidRule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	actor := f.ruleManager()

	// An event rule with duration_seconds > 0 is rejected by ValidateConditions today.
	id := f.insertLegacyRule(`{"duration_seconds": 30}`)

	off := false
	updated, err := f.svc.UpdateRule(ctx, actor, id, rules.UpdateRuleRequest{ExpectedVersion: 1, Enabled: &off})
	if err != nil {
		t.Fatalf("disabling a legacy-invalid rule: %v", err)
	}
	if updated.Enabled || updated.Version != 2 {
		t.Fatalf("updated = %+v, want disabled at version 2", updated)
	}
	vs := f.versions(id)
	if len(vs) != 1 || vs[0].Version != 2 {
		t.Fatalf("rule_versions = %+v, want a single row for version 2", vs)
	}

	// Touching conditions re-validates the merged rule and is still rejected.
	_, err = f.svc.UpdateRule(ctx, actor, id, rules.UpdateRuleRequest{
		ExpectedVersion: 2,
		Conditions:      &rules.Conditions{DurationSeconds: 60},
	})
	var ve *rules.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("invalid condition patch err = %v, want ValidationError", err)
	}
}

func TestRuleUpdateCorruptStoredConditionsSurfacesError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	actor := f.ruleManager()

	// Valid JSON of the wrong shape cannot decode into Conditions.
	id := f.insertLegacyRule(`{"labels": "person"}`)

	// A trigger change forces validation against the stored conditions.
	tr := rules.TriggerCameraOffline
	_, err := f.svc.UpdateRule(ctx, actor, id, rules.UpdateRuleRequest{ExpectedVersion: 1, TriggerType: &tr})
	if err == nil {
		t.Fatal("expected an error for corrupt stored conditions")
	}
	var ve *rules.ValidationError
	if errors.As(err, &ve) {
		t.Fatalf("err = %v, want an internal error, not ValidationError", err)
	}
}
