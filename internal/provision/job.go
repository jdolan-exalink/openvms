package provision

import (
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	stepConnecting = "connecting"
	stepPackages   = "packages"
	stepHardware   = "hardware"
	stepCompose    = "compose"
	stepNTP        = "ntp"
	stepAgent      = "agent"
	stepRegister   = "register"

	statePending = "pending"
	stateRunning = "running"
	stateDone    = "done"
	stateError   = "error"

	statusRunning   = "running"
	statusSucceeded = "succeeded"
	statusFailed    = "failed"
)

var stepOrder = []string{stepConnecting, stepPackages, stepHardware, stepCompose, stepNTP, stepAgent, stepRegister}

// StepState is one line of install progress.
type StepState struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Snapshot is the public view of an install. It has no credential fields.
type Snapshot struct {
	ID       uuid.UUID   `json:"id"`
	Status   string      `json:"status"`
	Steps    []StepState `json:"steps"`
	Warning  string      `json:"warning,omitempty"`
	Variant  string      `json:"variant,omitempty"`
	ServerID *uuid.UUID  `json:"server_id,omitempty"`
	Error    string      `json:"error,omitempty"`
	HostKey  string      `json:"host_key,omitempty"`
}

type job struct {
	mu    sync.Mutex
	owner uuid.UUID
	snap  Snapshot
	ip    string
}

func newJob(owner uuid.UUID, ip string) *job {
	steps := make([]StepState, len(stepOrder))
	for i, id := range stepOrder {
		steps[i] = StepState{ID: id, State: statePending}
	}
	return &job{
		owner: owner,
		ip:    ip,
		snap: Snapshot{
			ID:     uuid.New(),
			Status: statusRunning,
			Steps:  steps,
		},
	}
}

func (j *job) snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := j.snap
	out.Steps = append([]StepState(nil), j.snap.Steps...)
	return out
}

func (j *job) setStep(id, state, detail string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range j.snap.Steps {
		if j.snap.Steps[i].ID == id {
			j.snap.Steps[i].State = state
			j.snap.Steps[i].Detail = detail
		}
	}
}

func (j *job) addWarning(warning string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.snap.Warning == warning || strings.Contains(j.snap.Warning, warning) {
		return
	}
	if j.snap.Warning == "" {
		j.snap.Warning = warning
		return
	}
	if (j.snap.Warning == "cpu" && warning == "system_disk") || (j.snap.Warning == "system_disk" && warning == "cpu") {
		j.snap.Warning = "cpu_system_disk"
	}
}

func (j *job) warning() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.Warning
}

func (j *job) setVariant(v Variant) {
	j.mu.Lock()
	j.snap.Variant = string(v)
	j.mu.Unlock()
}

func (j *job) setHostKey(fp string) {
	j.mu.Lock()
	j.snap.HostKey = fp
	j.mu.Unlock()
}

func (j *job) setServer(id uuid.UUID) {
	j.mu.Lock()
	j.snap.ServerID = &id
	j.mu.Unlock()
}

func (j *job) fail(step, detail, secret string) {
	detail = Scrub(detail, secret)
	j.mu.Lock()
	defer j.mu.Unlock()
	j.snap.Status = statusFailed
	j.snap.Error = detail
	for i := range j.snap.Steps {
		if j.snap.Steps[i].ID == step && j.snap.Steps[i].State != stateDone {
			j.snap.Steps[i].State = stateError
			j.snap.Steps[i].Detail = detail
		}
	}
}

func (j *job) succeed() {
	j.mu.Lock()
	j.snap.Status = statusSucceeded
	j.mu.Unlock()
}
