package camconfig

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// The tour runner executes a stored PTZ tour: for each step it goes to the device preset and waits
// the dwell time, repeating the sequence. It lives in the memory of the API process: a restart of
// the API stops the running tour (the stored tour is not touched), and with several API replicas
// each one only knows the tours it started. One tour runs per camera. Any manual movement command
// cancels it (the handler calls Cancel), and Close cancels all of them on shutdown.

// Reasons a tour stopped (TourStatus.StopReason).
const (
	TourStopUser          = "user"
	TourStopManualCommand = "manual_command"
	TourStopTourChanged   = "tour_changed"
	TourStopShutdown      = "shutdown"
	TourStopError         = "error"
	TourStopDone          = "done"
)

// Tour states (TourStatus.State).
const (
	TourIdle     = "idle"
	TourRunning  = "running"
	TourFinished = "finished"
	TourStopped  = "stopped"
	TourFailed   = "failed"
)

// minTourStepInterval keeps a tour of zero-dwell steps from hammering the device.
const minTourStepInterval = time.Second

// ErrTourRunning means the camera already runs a tour.
var ErrTourRunning = errors.New("the camera already runs a PTZ tour")

// TourStep is one stop of a tour. Speed is stored with the tour; going to a preset does not take a
// speed in the ONVIF call the runner uses, so it is carried but not applied.
type TourStep struct {
	PresetToken string
	Dwell       time.Duration
	Speed       float64
}

// Tour is what the runner executes.
type Tour struct {
	ID     uuid.UUID
	Name   string
	Steps  []TourStep
	Repeat int // 0 repeats until stopped
}

// TourMove sends the camera to a step's preset through the same PTZ path as a manual command.
type TourMove func(ctx context.Context, step TourStep) error

// TourStatus is the last known state of a camera's tour.
type TourStatus struct {
	State      string
	TourID     uuid.UUID
	TourName   string
	StartedAt  time.Time
	EndedAt    time.Time
	Step       int
	Round      int
	StopReason string
	// Error is a short code, never a device message.
	Error string
}

type tourRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	status TourStatus
	reason string // set by Cancel before the context ends
}

// TourRunner runs at most one tour per camera.
type TourRunner struct {
	mu     sync.Mutex
	runs   map[uuid.UUID]*tourRun
	closed bool
	wg     sync.WaitGroup
	now    func() time.Time
	// sleep waits d or until ctx ends; tests replace it.
	sleep func(ctx context.Context, d time.Duration) bool
}

// NewTourRunner returns an empty runner.
func NewTourRunner() *TourRunner {
	return &TourRunner{runs: map[uuid.UUID]*tourRun{}, now: time.Now, sleep: sleepContext}
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Start begins the tour on the camera. It returns ErrTourRunning while the camera runs another one
// and an error for a tour without steps.
func (r *TourRunner) Start(cameraID uuid.UUID, tour Tour, move TourMove) (TourStatus, error) {
	if len(tour.Steps) == 0 || move == nil {
		return TourStatus{}, errors.New("a tour needs steps and a way to move the camera")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return TourStatus{}, errors.New("the tour runner is closed")
	}
	if cur := r.runs[cameraID]; cur != nil && cur.status.State == TourRunning {
		return TourStatus{}, ErrTourRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &tourRun{cancel: cancel, done: make(chan struct{}), status: TourStatus{State: TourRunning, TourID: tour.ID, TourName: tour.Name, StartedAt: r.now()}}
	r.runs[cameraID] = run
	r.wg.Add(1)
	go r.loop(ctx, run, tour, move)
	return run.status, nil
}

func (r *TourRunner) loop(ctx context.Context, run *tourRun, tour Tour, move TourMove) {
	defer r.wg.Done()
	defer close(run.done)
	for round := 0; tour.Repeat == 0 || round < tour.Repeat; round++ {
		for i, step := range tour.Steps {
			r.update(run, func(s *TourStatus) { s.Step, s.Round = i, round })
			if err := move(ctx, step); err != nil {
				if ctx.Err() != nil {
					r.finish(run, TourStopped, "")
					return
				}
				r.finishWith(run, TourFailed, TourStopError, "move_failed")
				return
			}
			wait := step.Dwell
			if wait < minTourStepInterval {
				wait = minTourStepInterval
			}
			if !r.sleep(ctx, wait) {
				r.finish(run, TourStopped, "")
				return
			}
		}
	}
	r.finishWith(run, TourFinished, TourStopDone, "")
}

func (r *TourRunner) update(run *tourRun, f func(*TourStatus)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&run.status)
}

// finish ends a run cancelled from outside with the reason Cancel recorded.
func (r *TourRunner) finish(run *tourRun, state, errCode string) {
	r.mu.Lock()
	reason := run.reason
	r.mu.Unlock()
	if reason == "" {
		reason = TourStopShutdown
	}
	r.finishWith(run, state, reason, errCode)
}

func (r *TourRunner) finishWith(run *tourRun, state, reason, errCode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.status.State, run.status.StopReason, run.status.Error, run.status.EndedAt = state, reason, errCode, r.now()
}

// Cancel stops the camera's running tour (if any) for the reason and waits for it to end. It
// reports whether a tour was running.
func (r *TourRunner) Cancel(cameraID uuid.UUID, reason string) bool {
	r.mu.Lock()
	run := r.runs[cameraID]
	if run == nil || run.status.State != TourRunning || run.reason != "" {
		r.mu.Unlock()
		return false
	}
	run.reason = reason
	r.mu.Unlock()
	run.cancel()
	<-run.done
	return true
}

// CancelTour stops the camera's tour only when it is the given one (a tour that is edited or
// deleted stops; another tour keeps running).
func (r *TourRunner) CancelTour(cameraID, tourID uuid.UUID, reason string) bool {
	r.mu.Lock()
	run := r.runs[cameraID]
	match := run != nil && run.status.State == TourRunning && run.status.TourID == tourID
	r.mu.Unlock()
	if !match {
		return false
	}
	return r.Cancel(cameraID, reason)
}

// Status returns the camera's last tour state; a camera that never ran one is idle.
func (r *TourRunner) Status(cameraID uuid.UUID) TourStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run := r.runs[cameraID]; run != nil {
		return run.status
	}
	return TourStatus{State: TourIdle}
}

// Close cancels every running tour and refuses new ones; call it on shutdown.
func (r *TourRunner) Close() {
	r.mu.Lock()
	r.closed = true
	for _, run := range r.runs {
		if run.status.State == TourRunning && run.reason == "" {
			run.reason = TourStopShutdown
			run.cancel()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}
