package agent

import (
	"testing"
	"time"
)

// failsafe only bounds a test that would otherwise hang; no assertion depends on it.
const failsafe = 30 * time.Second

// beatDriver drives a Worker by hand: the test decides when a heartbeat tick happens and sees the
// outcome of every attempt, so no test sleeps or polls for timing.
type beatDriver struct {
	ticks chan time.Time
	beats chan error
}

func newBeatDriver() *beatDriver {
	return &beatDriver{ticks: make(chan time.Time), beats: make(chan error, 256)}
}

// wire connects the driver to cfg's ticker and heartbeat hook.
func (d *beatDriver) wire(cfg *WorkerConfig) {
	cfg.NewTicker = func(time.Duration) (<-chan time.Time, func()) { return d.ticks, func() {} }
	cfg.OnHeartbeat = func(err error) { d.beats <- err }
}

// next waits for the outcome of the next heartbeat attempt (the initial one, first).
func (d *beatDriver) next(t *testing.T) error {
	t.Helper()
	select {
	case err := <-d.beats:
		return err
	case <-time.After(failsafe):
		t.Fatal("no heartbeat attempt")
		return nil
	}
}

// tick delivers one heartbeat tick and returns the outcome of the attempt it causes.
func (d *beatDriver) tick(t *testing.T) error {
	t.Helper()
	select {
	case d.ticks <- time.Now():
	case <-time.After(failsafe):
		t.Fatal("the worker did not take a tick")
	}
	return d.next(t)
}
