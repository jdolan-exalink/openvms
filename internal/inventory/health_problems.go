package inventory

import (
	"math"
	"strings"

	"github.com/jdolan-exalink/openvms/internal/frigate"
)

// Reasons a server is reported as degraded. The UI turns each code into a sentence.
const (
	ProblemCamerasOffline   = "cameras_offline"
	ProblemStorageHigh      = "storage_high"
	ProblemDetectionStalled = "detection_stalled"
	// ProblemRecorderDisk: a Provision recorder reports a disk as bad.
	ProblemRecorderDisk = "recorder_disk_bad"
)

// HealthProblem is one reason a server is degraded.
type HealthProblem struct {
	Code        string   `json:"code"`
	Cameras     []string `json:"cameras,omitempty"`
	UsedPercent int      `json:"used_percent,omitempty"`
}

// isVirtualCamera reports Frigate channels that are not physical cameras. BirdEye is a mosaic
// without stats, so it would otherwise always look offline.
func isVirtualCamera(remoteName string) bool {
	return strings.EqualFold(remoteName, "birdseye")
}

// healthProblems lists why a server is degraded, in a stable order. An empty result means healthy.
func healthProblems(offlineCameras []string, rec *frigate.Storage, detectionStalled bool) []HealthProblem {
	var out []HealthProblem
	if len(offlineCameras) > 0 {
		out = append(out, HealthProblem{Code: ProblemCamerasOffline, Cameras: offlineCameras})
	}
	if rec != nil && rec.TotalMB > 0 && rec.UsedMB/rec.TotalMB > storageDegradedRatio {
		out = append(out, HealthProblem{Code: ProblemStorageHigh, UsedPercent: int(math.Round(rec.UsedMB / rec.TotalMB * 100))})
	}
	if detectionStalled {
		out = append(out, HealthProblem{Code: ProblemDetectionStalled})
	}
	return out
}
