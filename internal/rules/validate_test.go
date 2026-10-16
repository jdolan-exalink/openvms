package rules

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func intp(v int) *int { return &v }

func TestValidateConditions(t *testing.T) {
	id := []uuid.UUID{uuid.New()}
	tests := []struct {
		name    string
		trigger TriggerType
		cond    Conditions
		wantErr string // substring; empty means valid
	}{
		{"event with labels and groups", TriggerEvent, Conditions{Labels: []string{"person"}, CameraGroupIDs: id, MapZoneIDs: id}, ""},
		{"event rejects server_ids", TriggerEvent, Conditions{ServerIDs: id}, "server_ids"},
		{"event rejects duration_seconds", TriggerEvent, Conditions{DurationSeconds: 60}, "duration_seconds"},
		{"camera_offline allows camera_ids and duration", TriggerCameraOffline, Conditions{CameraIDs: id, SiteIDs: id, DurationSeconds: 60}, ""},
		{"camera_offline rejects server_ids", TriggerCameraOffline, Conditions{ServerIDs: id}, "server_ids"},
		{"camera_offline rejects labels", TriggerCameraOffline, Conditions{Labels: []string{"person"}}, "labels"},
		{"camera_offline rejects schedule", TriggerCameraOffline, Conditions{Schedule: &RuleSchedule{DaysOfWeek: []int{1}}}, "schedule"},
		{"camera_offline rejects cooldown", TriggerCameraOffline, Conditions{CooldownSeconds: intp(60)}, "cooldown_seconds"},
		{"server_offline allows server_ids", TriggerServerOffline, Conditions{ServerIDs: id, DurationSeconds: 30}, ""},
		{"server_offline rejects camera_ids", TriggerServerOffline, Conditions{CameraIDs: id}, "camera_ids"},
		{"server_offline rejects camera_group_ids", TriggerServerOffline, Conditions{CameraGroupIDs: id}, "camera_group_ids"},
		{"negative duration", TriggerCameraOffline, Conditions{DurationSeconds: -1}, "duration_seconds"},
		{"cooldown zero is default", TriggerEvent, Conditions{CooldownSeconds: intp(0)}, ""},
		{"cooldown max", TriggerEvent, Conditions{CooldownSeconds: intp(MaxCooldownSeconds)}, ""},
		{"cooldown negative", TriggerEvent, Conditions{CooldownSeconds: intp(-1)}, "cooldown_seconds"},
		{"cooldown above bound", TriggerEvent, Conditions{CooldownSeconds: intp(MaxCooldownSeconds + 1)}, "cooldown_seconds"},
		{"valid schedule", TriggerEvent, Conditions{Schedule: &RuleSchedule{DaysOfWeek: []int{0, 6}, StartTime: "22:00", EndTime: "06:00", Timezone: "America/Argentina/Buenos_Aires"}}, ""},
		{"schedule bad start", TriggerEvent, Conditions{Schedule: &RuleSchedule{StartTime: "25:00", EndTime: "06:00"}}, "start_time"},
		{"schedule bad end format", TriggerEvent, Conditions{Schedule: &RuleSchedule{StartTime: "08:00", EndTime: "6am"}}, "end_time"},
		{"schedule start without end", TriggerEvent, Conditions{Schedule: &RuleSchedule{StartTime: "08:00"}}, "start_time and end_time"},
		{"schedule day out of range", TriggerEvent, Conditions{Schedule: &RuleSchedule{DaysOfWeek: []int{7}}}, "days_of_week"},
		{"schedule negative day", TriggerEvent, Conditions{Schedule: &RuleSchedule{DaysOfWeek: []int{-1}}}, "days_of_week"},
		{"schedule unknown timezone", TriggerEvent, Conditions{Schedule: &RuleSchedule{Timezone: "Mars/Olympus"}}, "timezone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConditions(tt.trigger, tt.cond)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if _, ok := err.(*ValidationError); !ok {
				t.Fatalf("error type = %T, want *ValidationError", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tt.wantErr)
			}
		})
	}
}
