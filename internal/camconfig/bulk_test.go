package camconfig

import "testing"

func TestSectionAcceptsExactlyOne(t *testing.T) {
	on := true
	if s, err := Section(Patch{Detection: &DetectionPatch{Enabled: &on}}); err != nil || s != "detection" {
		t.Fatalf("section = %q, %v", s, err)
	}
	if _, err := Section(Patch{Detection: &DetectionPatch{Enabled: &on}, Recording: &RecordingPatch{Enabled: &on}}); err == nil {
		t.Fatal("two sections accepted")
	}
	if _, err := Section(Patch{}); err == nil {
		t.Fatal("empty patch accepted")
	}
}

func TestSummarizeCountsStatusesAndRestarts(t *testing.T) {
	rows := []BulkRow{
		{Status: BulkWillApply, Impact: []ImpactItem{{Code: ImpactEngineRestart}}},
		{Status: BulkWillApply},
		{Status: BulkIncompatible, Impact: []ImpactItem{{Code: ImpactEngineRestart}}},
		{Status: BulkSkipped}, {Status: BulkUnreachable},
	}
	r := Summarize("recording", rows)
	if r.Totals.Cameras != 5 || r.Totals.WillApply != 2 || r.Totals.Incompatible != 1 || r.Totals.Skipped != 1 || r.Totals.Unreachable != 1 || r.Restarts != 1 {
		t.Fatalf("report = %+v", r)
	}
}
