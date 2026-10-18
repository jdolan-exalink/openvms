//go:build integration

package rules_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jdolan-exalink/openvms/internal/access"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

// person inserts a live registered person and returns its id.
func (f *fixture) person(display, frigateName string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.scan(&id, `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1, $2, $3) RETURNING id`, f.env.Demo.TenantID, display, frigateName); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *fixture) watchlist(name string, members ...uuid.UUID) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.scan(&id, `INSERT INTO face_watchlists (tenant_id, name) VALUES ($1, $2) RETURNING id`, f.env.Demo.TenantID, name); err != nil {
		f.t.Fatal(err)
	}
	for _, m := range members {
		if err := f.exec(`INSERT INTO face_watchlist_members (tenant_id, watchlist_id, person_id) VALUES ($1, $2, $3)`, f.env.Demo.TenantID, id, m); err != nil {
			f.t.Fatal(err)
		}
	}
	return id
}

// expireFaces drops the resolver's cached snapshot, standing in for the TTL elapsing.
func (f *fixture) expireFaces() {
	f.t.Helper()
	r, ok := f.svc.Faces.(*rules.StoreFaceResolver)
	if !ok {
		f.t.Fatalf("service face resolver is %T, want *rules.StoreFaceResolver", f.svc.Faces)
	}
	r.Expire()
}

func (f *fixture) faceEvent(subLabels ...string) rules.EventContext {
	ev := f.eventCtx("person")
	ev.SubLabels = subLabels
	return ev
}

func TestEvaluateEventMatchesRecognizedPersons(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	f.person("Beto", "beto")
	rule := f.addRule("Vigilancia", rules.TriggerEvent, rules.Conditions{FacePersonIDs: []uuid.UUID{ana}}, rules.Actions{NotifyInApp: true}, true)

	for _, sub := range [][]string{nil, {"beto"}, {"Ana"}, {"carla", "beto"}} {
		if err := f.svc.EvaluateEvent(ctx, f.faceEvent(sub...)); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.notifications(rule); n != 0 {
		t.Fatalf("events without the listed person created %d notifications, want 0", n)
	}
	if err := f.svc.EvaluateEvent(ctx, f.faceEvent("zzz", "ana")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 1 {
		t.Fatalf("recognized person created %d notifications, want 1", n)
	}
	// The notification must not carry the person's name.
	var title, body string
	if err := f.scan(&body, `SELECT body FROM notifications WHERE rule_id = $1`, rule); err != nil {
		t.Fatal(err)
	}
	if err := f.scan(&title, `SELECT title FROM notifications WHERE rule_id = $1`, rule); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(body+title), "ana") {
		t.Fatalf("notification leaks the name: %q / %q", title, body)
	}
}

func TestEvaluateEventWatchlistResolvedAtEvaluationAndCombinedWithPersons(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	beto := f.person("Beto", "beto")
	carla := f.person("Carla", "carla")
	list := f.watchlist("VIP", ana)
	rule := f.addRule("VIP o Carla", rules.TriggerEvent, rules.Conditions{FaceWatchlistIDs: []uuid.UUID{list}, FacePersonIDs: []uuid.UUID{carla}},
		rules.Actions{NotifyInApp: true}, true)
	f.svc.Debounce = time.Nanosecond

	expect := func(label string, want int, sub ...string) {
		t.Helper()
		if err := f.svc.EvaluateEvent(ctx, f.faceEvent(sub...)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
		if n := f.notifications(rule); n != want {
			t.Fatalf("%s: notifications = %d, want %d", label, n, want)
		}
	}
	expect("member of the watchlist", 1, "ana")
	expect("listed person outside the watchlist", 2, "carla")
	expect("unlisted person", 2, "beto")

	// Membership changes apply once the resolver cache expires.
	if err := f.exec(`INSERT INTO face_watchlist_members (tenant_id, watchlist_id, person_id) VALUES ($1, $2, $3)`, f.env.Demo.TenantID, list, beto); err != nil {
		t.Fatal(err)
	}
	f.expireFaces()
	expect("new member after cache expiry", 3, "beto")

	// A deleted person drops out even while its row is still a tombstone.
	if err := f.exec(`UPDATE face_persons SET deleted_at = now() WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	f.expireFaces()
	expect("tombstoned person", 3, "ana")
}

func TestEvaluateEventPersonRuleKeepsOtherConditionsAndCooldown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	other := f.env.Cameras["frigate-h01/plaza"]
	cam := f.env.Cameras["frigate-h01/acceso_norte"]
	rule := f.addRule("Ana en acceso", rules.TriggerEvent, rules.Conditions{FacePersonIDs: []uuid.UUID{ana}, CameraIDs: []uuid.UUID{cam.ID}},
		rules.Actions{NotifyInApp: true}, true)

	wrong := f.faceEvent("ana")
	wrong.CameraID, wrong.CameraName = other.ID, other.DisplayName
	if err := f.svc.EvaluateEvent(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 0 {
		t.Fatalf("person on another camera notified %d times, want 0", n)
	}
	for i := 0; i < 3; i++ {
		if err := f.svc.EvaluateEvent(ctx, f.faceEvent("ana")); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.notifications(rule); n != 1 {
		t.Fatalf("three sightings inside the cooldown notified %d times, want 1", n)
	}
}

func TestEvaluateEventFaceUpdateRunsOnlyPersonRules(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	plain := f.addRule("Personas", rules.TriggerEvent, rules.Conditions{Labels: []string{"person"}}, rules.Actions{NotifyInApp: true}, true)
	face := f.addRule("Ana", rules.TriggerEvent, rules.Conditions{FacePersonIDs: []uuid.UUID{ana}}, rules.Actions{NotifyInApp: true}, true)

	ev := f.faceEvent("ana")
	ev.FaceUpdate = true
	if err := f.svc.EvaluateEvent(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(plain); n != 0 {
		t.Fatalf("a face update re-ran a rule without person conditions (%d notifications)", n)
	}
	if n := f.notifications(face); n != 1 {
		t.Fatalf("a face update did not run the person rule (%d notifications)", n)
	}
}

func TestEvaluateEventPersonRuleFailsClosedWithoutResolver(t *testing.T) {
	f := newFixture(t)
	ana := f.person("Ana", "ana")
	rule := f.addRule("Ana", rules.TriggerEvent, rules.Conditions{FacePersonIDs: []uuid.UUID{ana}}, rules.Actions{NotifyInApp: true}, true)
	f.svc.Faces = nil
	if err := f.svc.EvaluateEvent(context.Background(), f.faceEvent("ana")); err != nil {
		t.Fatal(err)
	}
	if n := f.notifications(rule); n != 0 {
		t.Fatalf("no resolver: %d notifications, want 0", n)
	}
}

func (f *fixture) faceUser(perms ...authz.Permission) authz.Actor {
	f.t.Helper()
	ctx := context.Background()
	tenantID := f.env.Demo.TenantID
	actor, _, err := bootstrap.TenantUser(ctx, f.env.Store, tenantID, "gestor-"+uuid.NewString()[:8])
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range append([]authz.Permission{authz.RulesManage, authz.RulesRead}, perms...) {
		if _, err := f.env.Svc.CreateGrant(ctx, f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: actor.UserID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenantID,
		}); err != nil {
			f.t.Fatal(err)
		}
	}
	return actor
}

func TestRuleServiceValidatesFaceReferencesAndPermission(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	list := f.watchlist("VIP", ana)
	withFaces := f.faceUser(authz.FacesView)
	without := f.faceUser()
	req := func(c rules.Conditions) rules.CreateRuleRequest {
		return rules.CreateRuleRequest{Name: "r-" + uuid.NewString()[:4], TriggerType: rules.TriggerEvent, Enabled: true, Conditions: c}
	}

	if _, err := f.svc.CreateRule(ctx, withFaces, req(rules.Conditions{FacePersonIDs: []uuid.UUID{ana}, FaceWatchlistIDs: []uuid.UUID{list}})); err != nil {
		t.Fatalf("create with faces.view = %v", err)
	}
	if _, err := f.svc.CreateRule(ctx, without, req(rules.Conditions{FacePersonIDs: []uuid.UUID{ana}})); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("create person condition without faces.view = %v, want forbidden", err)
	}
	if _, err := f.svc.CreateRule(ctx, without, req(rules.Conditions{FaceWatchlistIDs: []uuid.UUID{list}})); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("create watchlist condition without faces.view = %v, want forbidden", err)
	}
	var bad *rules.ValidationError
	if _, err := f.svc.CreateRule(ctx, withFaces, req(rules.Conditions{FacePersonIDs: []uuid.UUID{uuid.New()}})); !errors.As(err, &bad) {
		t.Fatalf("unknown person = %v, want validation error", err)
	}
	if _, err := f.svc.CreateRule(ctx, withFaces, req(rules.Conditions{FaceWatchlistIDs: []uuid.UUID{uuid.New()}})); !errors.As(err, &bad) {
		t.Fatalf("unknown watchlist = %v, want validation error", err)
	}
	if err := f.exec(`UPDATE face_persons SET deleted_at = now() WHERE id = $1`, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateRule(ctx, withFaces, req(rules.Conditions{FacePersonIDs: []uuid.UUID{ana}})); !errors.As(err, &bad) {
		t.Fatalf("deleted person = %v, want validation error", err)
	}
}

func TestRuleUpdateKeepsFaceConditionsForEditorsWithoutFacesView(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ana := f.person("Ana", "ana")
	beto := f.person("Beto", "beto")
	withFaces := f.faceUser(authz.FacesView)
	without := f.faceUser()
	created, err := f.svc.CreateRule(ctx, withFaces, rules.CreateRuleRequest{Name: "Ana", TriggerType: rules.TriggerEvent, Enabled: true,
		Conditions: rules.Conditions{FacePersonIDs: []uuid.UUID{ana}, Labels: []string{"person"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Re-sending the stored conditions (an unrelated edit) works without faces.view...
	same := created.Conditions
	same.Labels = []string{"person", "car"}
	if _, err := f.svc.UpdateRule(ctx, without, created.ID, rules.UpdateRuleRequest{Conditions: &same}); err != nil {
		t.Fatalf("edit that keeps the person condition = %v", err)
	}
	// ...but changing the person condition does not.
	changed := same
	changed.FacePersonIDs = []uuid.UUID{beto}
	if _, err := f.svc.UpdateRule(ctx, without, created.ID, rules.UpdateRuleRequest{Conditions: &changed}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("changing the person condition without faces.view = %v, want forbidden", err)
	}
	// Dropping it entirely is also a change that needs faces.view.
	cleared := same
	cleared.FacePersonIDs = nil
	if _, err := f.svc.UpdateRule(ctx, without, created.ID, rules.UpdateRuleRequest{Conditions: &cleared}); !errors.Is(err, access.ErrForbidden) {
		t.Fatalf("clearing the person condition without faces.view = %v, want forbidden", err)
	}
	if _, err := f.svc.UpdateRule(ctx, withFaces, created.ID, rules.UpdateRuleRequest{Conditions: &changed}); err != nil {
		t.Fatalf("change with faces.view = %v", err)
	}
}

// EvaluateEvent holds a transaction while it resolves the face conditions. With a single pool
// connection a resolver that opens its own transaction would wait forever for a second one, so
// this only passes when the resolver reads through the evaluation transaction.
func TestEvaluateEventResolvesFacesInsideTheEvaluationTransaction(t *testing.T) {
	f := newFixture(t)
	ana := f.person("Ana", "ana")
	rule := f.addRule("Vigilancia", rules.TriggerEvent, rules.Conditions{FacePersonIDs: []uuid.UUID{ana}}, rules.Actions{NotifyInApp: true}, true)

	cfg := f.env.Pool.Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	svc := rules.NewService(&store.Store{Pool: pool}, &recordingPublisher{}, pgtest.Discard()) // empty face cache

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.EvaluateEvent(ctx, f.faceEvent("ana")); err != nil {
		t.Fatalf("evaluation with a single pooled connection: %v", err)
	}
	if n := f.notifications(rule); n != 1 {
		t.Fatalf("recognized person created %d notifications, want 1", n)
	}
}

// A watchlist delete in flight holds the row; a rule create referencing it must wait for the
// delete and then be refused, instead of committing a rule that points at a removed list.
func TestRuleCreateWaitsForAConcurrentWatchlistDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	list := f.watchlist("VIP")
	actor := f.faceUser(authz.FacesView)

	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock() // a failed assertion must not leave the holder transaction open
	go func() {
		holder <- f.env.Store.TxRaw(ctx, store.AllTenants, func(tx pgx.Tx) error {
			// What a watchlist delete does: lock the row exclusively, then remove it.
			if _, err := tx.Exec(ctx, `SELECT id FROM face_watchlists WHERE id = $1 FOR UPDATE`, list); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM face_watchlists WHERE id = $1`, list); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	type result struct{ err error }
	created := make(chan result, 1)
	go func() {
		_, err := f.svc.CreateRule(ctx, actor, rules.CreateRuleRequest{Name: "vip", TriggerType: rules.TriggerEvent, Enabled: true,
			Conditions: rules.Conditions{FaceWatchlistIDs: []uuid.UUID{list}}})
		created <- result{err}
	}()
	select {
	case r := <-created:
		t.Fatalf("rule create finished while the watchlist delete was in flight: %v", r.err)
	case <-time.After(500 * time.Millisecond):
	}
	unblock()
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	var bad *rules.ValidationError
	if r := <-created; !errors.As(r.err, &bad) {
		t.Fatalf("rule create after the delete committed = %v, want validation error", r.err)
	}
}
