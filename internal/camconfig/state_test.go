package camconfig

import (
	"errors"
	"testing"
)

func TestComputeState(t *testing.T) {
	cam := readyCamera() // detection off, recording on
	want := Patch{Detection: &DetectionPatch{Enabled: bp(true)}}
	matching := Patch{Detection: &DetectionPatch{Enabled: bp(false)}}
	down := Camera{Sources: []Source{{Name: "frigate", Status: SourceUnavailable}}}

	for name, tc := range map[string]struct {
		st    Stored
		cam   Camera
		state string
		drift int
	}{
		"never configured is synced":       {Stored{SyncState: SyncSynced}, cam, SyncSynced, 0},
		"desired equals effective":         {Stored{Desired: matching, DesiredRevision: 2, AppliedRevision: 2, SyncState: SyncSynced}, cam, SyncSynced, 0},
		"external change is drift":         {Stored{Desired: want, DesiredRevision: 2, AppliedRevision: 2, SyncState: SyncSynced}, cam, SyncDrift, 1},
		"differences with apply in flight": {Stored{Desired: want, DesiredRevision: 3, AppliedRevision: 2, SyncState: SyncPending}, cam, SyncPending, 1},
		"recorded failure stays error":     {Stored{Desired: want, DesiredRevision: 3, AppliedRevision: 2, SyncState: SyncError, LastError: "x"}, cam, SyncError, 1},
		"engine unreadable":                {Stored{Desired: want, DesiredRevision: 2, AppliedRevision: 2, SyncState: SyncSynced}, down, SyncUnverifiable, 0},
	} {
		t.Run(name, func(t *testing.T) {
			got := ComputeState(tc.st, tc.cam)
			if got.SyncState != tc.state || len(got.Drift) != tc.drift {
				t.Fatalf("state = %+v, want %s with %d drift", got, tc.state, tc.drift)
			}
			if got.DesiredRevision != tc.st.DesiredRevision || got.AppliedRevision != tc.st.AppliedRevision {
				t.Fatalf("revisions = %+v", got)
			}
		})
	}
}

func TestDriftReportsDesiredAndEffective(t *testing.T) {
	objs := []string{"person", "car"}
	d := Diff(Patch{Detection: &DetectionPatch{Enabled: bp(true), Objects: &objs}}, readyCamera())
	if len(d) != 2 || d[0].Path != "detection.enabled" || d[0].Desired != true || d[0].Effective != false {
		t.Fatalf("drift = %+v", d)
	}
	if d[1].Path != "detection.objects" {
		t.Fatalf("drift = %+v", d)
	}
	// A value the engine does not report is not agreement.
	if d := Diff(Patch{Recording: &RecordingPatch{Enabled: bp(true)}}, Camera{}); len(d) != 1 || d[0].Effective != nil {
		t.Fatalf("drift = %+v", d)
	}
}

func TestDecideOutcome(t *testing.T) {
	patch := Patch{Detection: &DetectionPatch{Enabled: bp(true)}}
	applied := readyCamera()
	applied.Detection.Enabled = bp(true)
	down := Camera{Sources: []Source{{Name: "frigate", Status: SourceUnavailable}}}

	t.Run("verified apply is synced and advances", func(t *testing.T) {
		o := DecideOutcome(nil, applied, patch, patch)
		if o.SyncState != SyncSynced || !o.Applied || o.Result != ResultOK {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("engine failure is error and never synced", func(t *testing.T) {
		// Even when the engine already holds the requested values.
		o := DecideOutcome(errors.New("boom"), applied, patch, patch)
		if o.SyncState != SyncError || o.Applied || o.LastError != "boom" || o.Result != ResultError {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("unreadable engine after apply is unverifiable", func(t *testing.T) {
		o := DecideOutcome(nil, down, patch, patch)
		if o.SyncState != SyncUnverifiable || o.Applied {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("engine that does not reflect the change is an error", func(t *testing.T) {
		o := DecideOutcome(nil, readyCamera(), patch, patch)
		if o.SyncState != SyncError || o.Applied {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("older desired fields that drifted leave drift", func(t *testing.T) {
		desired := Merge(patch, Patch{Recording: &RecordingPatch{Enabled: bp(false)}})
		o := DecideOutcome(nil, applied, patch, desired)
		if o.SyncState != SyncDrift || !o.Applied {
			t.Fatalf("%+v", o)
		}
	})
}
