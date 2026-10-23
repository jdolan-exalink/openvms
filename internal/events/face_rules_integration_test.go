//go:build integration

package events_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jdolan-exalink/openvms/internal/alarms"
	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/bootstrap"
	"github.com/jdolan-exalink/openvms/internal/frigatemock"
	"github.com/jdolan-exalink/openvms/internal/inventory"
	"github.com/jdolan-exalink/openvms/internal/rules"
	"github.com/jdolan-exalink/openvms/internal/store"
	"github.com/jdolan-exalink/openvms/internal/testutil/pgtest"
)

const faceName = "zaira_face"

// putRecognized is putLabelled with the recognized sub_labels Frigate attaches to the review.
func (f *alarmFixture) putRecognized(id, label string, start time.Time, sub ...string) {
	ts := float64(start.UnixMicro()) / 1e6
	f.env.Mocks["frigate-h01"].Server.Store.Put(frigatemock.Review{
		ID: id, Camera: "plaza", StartTime: ts, Severity: "alert",
		ThumbPath: "/media/frigate/clips/review/thumb-plaza-" + id + ".webp",
		Data: frigatemock.ReviewData{
			Detections: []string{fmt.Sprintf("%f-%s", ts, id)}, Objects: []string{label},
			SubLabels: sub, Zones: []string{}, Audio: []string{},
		},
	})
}

func (f *alarmFixture) addPerson(display, frigateName string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	err := f.env.Store.TxRaw(context.Background(), store.AllTenants, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `INSERT INTO face_persons (tenant_id, display_name, frigate_name) VALUES ($1, $2, $3) RETURNING id`,
			f.env.Demo.TenantID, display, frigateName).Scan(&id)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func newFaceRuleFixture(t *testing.T) (*alarmFixture, *rules.Service) {
	t.Helper()
	f := newAlarmFixture(t)
	svc := rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	f.enableAt(time.Now().Add(time.Hour)) // the built-in alarm stays out of the way
	return f, svc
}

func TestSyncerPersonRuleFiresWhenTheFaceIsRecognizedAtCreation(t *testing.T) {
	f, ruleSvc := newFaceRuleFixture(t)
	person := f.addPerson("Zaira", faceName)
	f.addRule(map[string]any{"face_person_ids": []string{person.String()}}, map[string]any{"create_alarm": true, "notify_in_app": true})

	f.putRecognized("face-1", "zebra", time.Now().Add(-time.Minute), faceName)
	f.putRecognized("face-0", "zebra", time.Now().Add(-2*time.Minute), "someone_else")
	syncAndEvaluateRuleJobs(f, ruleSvc)
	if n, a := f.counts(); n != 1 || a != 1 {
		t.Fatalf("recognized and unrecognized reviews: %d notifications, %d rule alarms, want 1 and 1", n, a)
	}
}

// Frigate usually names the person after it opened the review, so the name arrives on an update.
func TestSyncerPersonRuleFiresOnceWhenTheNameArrivesLater(t *testing.T) {
	f, svc := newFaceRuleFixture(t)
	person := f.addPerson("Zaira", faceName)
	start := time.Now().Add(-time.Minute)
	f.addRule(map[string]any{"face_person_ids": []string{person.String()}}, map[string]any{"create_alarm": true, "notify_in_app": true})
	f.addRule(map[string]any{"labels": []string{"zebra"}}, map[string]any{"notify_in_app": true})

	f.putRecognized("face-2", "zebra", start)
	syncAndEvaluateRuleJobs(f, svc)
	if n, a := f.counts(); n != 1 || a != 0 {
		t.Fatalf("before recognition: %d notifications, %d rule alarms, want 1 (plain rule) and 0", n, a)
	}
	f.putRecognized("face-2", "zebra", start, faceName)
	syncAndEvaluateRuleJobs(f, svc)
	if n, a := f.counts(); n != 2 || a != 1 {
		t.Fatalf("after recognition: %d notifications, %d rule alarms, want 2 and 1 (plain rule must not fire again)", n, a)
	}
	// Further polls of the same review change nothing; a second name on the same review neither.
	f.putRecognized("face-2", "zebra", start, faceName, "extra_person")
	syncAndEvaluateRuleJobs(f, svc)
	syncAndEvaluateRuleJobs(f, svc)
	if n, a := f.counts(); n != 2 || a != 1 {
		t.Fatalf("after more polls: %d notifications, %d rule alarms, want 2 and 1", n, a)
	}
}

// A second person recognized on the same review re-evaluates the person rules with the gained name
// only: the rule of the first person must not fire again once its debounce window has passed.
func TestSyncerFaceUpdateEvaluatesOnlyTheGainedNames(t *testing.T) {
	f, _ := newFaceRuleFixture(t)
	svc := rules.NewService(f.env.Store, nopPublisher{}, pgtest.Discard())
	svc.Debounce = time.Nanosecond // nothing is debounced: a repeated evaluation would fire again
	personA := f.addPerson("Zaira", faceName)
	personB := f.addPerson("Bruno", "bruno_face")
	f.addRule(map[string]any{"face_person_ids": []string{personA.String()}}, map[string]any{"notify_in_app": true})
	f.addRule(map[string]any{"face_person_ids": []string{personB.String()}}, map[string]any{"notify_in_app": true})
	start := time.Now().Add(-time.Minute)

	f.putRecognized("face-4", "zebra", start)
	syncAndEvaluateRuleJobs(f, svc)
	f.putRecognized("face-4", "zebra", start, faceName)
	syncAndEvaluateRuleJobs(f, svc)
	if n, _ := f.counts(); n != 1 {
		t.Fatalf("after the first name: %d notifications, want 1 (rule of the first person)", n)
	}
	f.putRecognized("face-4", "zebra", start, faceName, "bruno_face")
	syncAndEvaluateRuleJobs(f, svc)
	if n, _ := f.counts(); n != 2 {
		t.Fatalf("after the second name: %d notifications, want 2 (rule of the second person only; the first must not refire)", n)
	}
}

func TestSyncerPersonRuleAlarmRedactsTheNameWithoutFacesView(t *testing.T) {
	f, svc := newFaceRuleFixture(t)
	ctx := context.Background()
	person := f.addPerson("Zaira", faceName)
	f.addRule(map[string]any{"face_person_ids": []string{person.String()}}, map[string]any{"create_alarm": true})
	f.putRecognized("face-3", "zebra", time.Now().Add(-time.Minute), faceName)
	syncAndEvaluateRuleJobs(f, svc)

	tenant := f.env.Demo.TenantID
	grant := func(userID uuid.UUID, p authz.Permission) {
		t.Helper()
		if _, err := f.env.Svc.CreateGrant(ctx, f.env.Admin, inventory.GrantInput{
			SubjectType: "user", SubjectID: userID, Permission: p, Effect: authz.Allow, ScopeType: authz.ScopeTenant, ScopeID: &tenant,
		}); err != nil {
			t.Fatal(err)
		}
	}
	operator, _, err := bootstrap.TenantUser(ctx, f.env.Store, tenant, "op-"+uuid.NewString()[:6])
	if err != nil {
		t.Fatal(err)
	}
	grant(operator.UserID, authz.AlarmsView)
	alarmSvc := &alarms.Service{Store: f.env.Store}
	labelsOf := func() []string {
		page, err := alarmSvc.ListPage(ctx, operator, alarms.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range page.Items {
			if a.Source == "rule" {
				return a.EventSubLabels
			}
		}
		t.Fatal("rule alarm not listed")
		return nil
	}
	if got := labelsOf(); slices.Contains(got, faceName) {
		t.Fatalf("operator without faces.view sees %v", got)
	}
	grant(operator.UserID, authz.FacesView)
	if got := labelsOf(); !slices.Contains(got, faceName) {
		t.Fatalf("operator with faces.view sees %v, want the name", got)
	}
}
