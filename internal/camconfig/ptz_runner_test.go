package camconfig

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type moveLog struct {
	mu    sync.Mutex
	steps []string
	seen  chan string
	fail  string
}

func newMoveLog() *moveLog { return &moveLog{seen: make(chan string, 64)} }

func (m *moveLog) move(_ context.Context, s TourStep) error {
	m.mu.Lock()
	m.steps = append(m.steps, s.PresetToken)
	fail := m.fail
	m.mu.Unlock()
	m.seen <- s.PresetToken
	if fail == s.PresetToken {
		return errors.New("camera said admin:hunter2 is wrong")
	}
	return nil
}

func (m *moveLog) list() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.steps...)
}

// gatedRunner waits at each dwell until the test releases it (or closes the gate).
func gatedRunner() (*TourRunner, chan struct{}, *[]time.Duration) {
	gate := make(chan struct{})
	var waits []time.Duration
	var mu sync.Mutex
	r := NewTourRunner()
	r.sleep = func(ctx context.Context, d time.Duration) bool {
		mu.Lock()
		waits = append(waits, d)
		mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-gate:
			return true
		}
	}
	return r, gate, &waits
}

func twoStepTour(repeat int) Tour {
	return Tour{ID: uuid.New(), Name: "Ronda", Repeat: repeat, Steps: []TourStep{{PresetToken: "A", Dwell: 5 * time.Second}, {PresetToken: "B", Dwell: 0}}}
}

func waitState(t *testing.T, r *TourRunner, cam uuid.UUID, state string) TourStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.Status(cam); s.State == state {
			return s
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("state never became %s: %+v", state, r.Status(cam))
	return TourStatus{}
}

func TestTourRunsStepsInOrderForItsRepeatCountAndFinishes(t *testing.T) {
	r, gate, waits := gatedRunner()
	close(gate)
	ml := newMoveLog()
	cam := uuid.New()
	if _, err := r.Start(cam, twoStepTour(2), ml.move); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, r, cam, TourFinished)
	if got := ml.list(); len(got) != 4 || got[0] != "A" || got[1] != "B" || got[2] != "A" || got[3] != "B" {
		t.Fatalf("moves: %v", got)
	}
	if s.StopReason != TourStopDone || s.Round != 1 || s.EndedAt.IsZero() {
		t.Fatalf("status: %+v", s)
	}
	for _, w := range *waits {
		if w < minTourStepInterval {
			t.Fatalf("a dwell of %v is below the minimum interval", w)
		}
	}
}

func TestOnlyOneTourRunsPerCamera(t *testing.T) {
	r, gate, _ := gatedRunner()
	defer close(gate)
	defer r.Close()
	ml := newMoveLog()
	cam := uuid.New()
	if _, err := r.Start(cam, twoStepTour(0), ml.move); err != nil {
		t.Fatal(err)
	}
	<-ml.seen
	if _, err := r.Start(cam, twoStepTour(0), ml.move); !errors.Is(err, ErrTourRunning) {
		t.Fatalf("second start on the same camera: %v", err)
	}
	if _, err := r.Start(uuid.New(), twoStepTour(0), ml.move); err != nil {
		t.Fatalf("another camera must be free: %v", err)
	}
}

func TestCancelStopsTheTourAndSendsNoMoreCommands(t *testing.T) {
	r, gate, _ := gatedRunner()
	defer close(gate)
	ml := newMoveLog()
	cam := uuid.New()
	tour := twoStepTour(0)
	if _, err := r.Start(cam, tour, ml.move); err != nil {
		t.Fatal(err)
	}
	<-ml.seen
	if !r.Cancel(cam, TourStopManualCommand) {
		t.Fatal("a running tour must report it was cancelled")
	}
	s := r.Status(cam)
	if s.State != TourStopped || s.StopReason != TourStopManualCommand || s.TourID != tour.ID {
		t.Fatalf("status: %+v", s)
	}
	n := len(ml.list())
	time.Sleep(20 * time.Millisecond)
	if len(ml.list()) != n {
		t.Fatal("the tour kept moving the camera after the cancel")
	}
	if r.Cancel(cam, TourStopUser) {
		t.Fatal("cancelling a tour that is not running must report false")
	}
	if _, err := r.Start(cam, twoStepTour(1), ml.move); err != nil {
		t.Fatalf("a stopped camera can start another tour: %v", err)
	}
}

func TestCancelTourOnlyStopsTheMatchingTour(t *testing.T) {
	r, gate, _ := gatedRunner()
	defer close(gate)
	defer r.Close()
	ml := newMoveLog()
	cam := uuid.New()
	tour := twoStepTour(0)
	if _, err := r.Start(cam, tour, ml.move); err != nil {
		t.Fatal(err)
	}
	<-ml.seen
	if r.CancelTour(cam, uuid.New(), TourStopTourChanged) {
		t.Fatal("another tour's id must not stop this one")
	}
	if !r.CancelTour(cam, tour.ID, TourStopTourChanged) || r.Status(cam).StopReason != TourStopTourChanged {
		t.Fatalf("status: %+v", r.Status(cam))
	}
}

func TestAFailedMoveEndsTheTourWithACodeAndNoDeviceText(t *testing.T) {
	r, gate, _ := gatedRunner()
	close(gate)
	ml := newMoveLog()
	ml.fail = "B"
	cam := uuid.New()
	if _, err := r.Start(cam, twoStepTour(0), ml.move); err != nil {
		t.Fatal(err)
	}
	s := waitState(t, r, cam, TourFailed)
	if s.Error != "move_failed" || s.StopReason != TourStopError || s.Step != 1 {
		t.Fatalf("status: %+v", s)
	}
}

func TestCloseCancelsEveryTourAndRefusesNewOnes(t *testing.T) {
	r, gate, _ := gatedRunner()
	defer close(gate)
	ml := newMoveLog()
	a, b := uuid.New(), uuid.New()
	for _, c := range []uuid.UUID{a, b} {
		if _, err := r.Start(c, twoStepTour(0), ml.move); err != nil {
			t.Fatal(err)
		}
		<-ml.seen
	}
	r.Close()
	for _, c := range []uuid.UUID{a, b} {
		if s := r.Status(c); s.State != TourStopped || s.StopReason != TourStopShutdown {
			t.Fatalf("status: %+v", s)
		}
	}
	if _, err := r.Start(uuid.New(), twoStepTour(1), ml.move); err == nil {
		t.Fatal("a closed runner must refuse to start")
	}
}

func TestStartRejectsAnEmptyTour(t *testing.T) {
	if _, err := NewTourRunner().Start(uuid.New(), Tour{}, newMoveLog().move); err == nil {
		t.Fatal("a tour without steps must not start")
	}
	if s := NewTourRunner().Status(uuid.New()); s.State != TourIdle {
		t.Fatalf("a new camera is idle: %+v", s)
	}
}
