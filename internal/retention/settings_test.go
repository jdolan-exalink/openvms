package retention

import (
	"errors"
	"testing"
	"time"
)

func ptr(v int) *int { return &v }

func valid() Settings {
	return Settings{FaceUnknownDays: 30, PressureCleanup: true, PressurePercent: 85}
}

func TestValidateAcceptsTheDefaultsAndTheEdges(t *testing.T) {
	for _, s := range []Settings{
		valid(),
		{FaceUnknownDays: 0, PressurePercent: 50},
		{FaceUnknownDays: MaxDays, NotificationDeliveryDays: ptr(0), NotificationReadDays: ptr(MaxDays), PressurePercent: 99},
	} {
		if err := s.Validate(); err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
	}
}

func TestValidateNamesTheFieldThatIsOutOfRange(t *testing.T) {
	cases := map[string]Settings{
		"face_unknown_days":          {FaceUnknownDays: -1, PressurePercent: 85},
		"notification_delivery_days": {FaceUnknownDays: 30, NotificationDeliveryDays: ptr(MaxDays + 1), PressurePercent: 85},
		"notification_read_days":     {FaceUnknownDays: 30, NotificationReadDays: ptr(-5), PressurePercent: 85},
		"pressure_percent":           {FaceUnknownDays: 30, PressurePercent: 100},
	}
	for field, s := range cases {
		err := s.Validate()
		if !errors.Is(err, ErrInvalid) || err == nil || !contains(err.Error(), field) {
			t.Fatalf("%s: err = %v, want ErrInvalid naming the field", field, err)
		}
	}
	low := valid()
	low.PressurePercent = 49
	if low.Validate() == nil {
		t.Fatal("pressure_percent 49 must be rejected")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPressureDaysHalvesRoundingUpAndNeverBelowOne(t *testing.T) {
	for days, want := range map[int]int{30: 15, 7: 4, 2: 1, 1: 1, 0: 15, 3650: 1825} {
		if got := PressureDays(days); got != want {
			t.Errorf("PressureDays(%d) = %d, want %d", days, got, want)
		}
	}
}

func TestNotificationRetentionMapsTheStoredValue(t *testing.T) {
	def := 30 * 24 * time.Hour
	if got := NotificationRetention(nil, def); got != def {
		t.Errorf("nil keeps the default, got %v", got)
	}
	if got := NotificationRetention(ptr(0), def); got >= 0 {
		t.Errorf("0 must disable the purge (negative), got %v", got)
	}
	if got := NotificationRetention(ptr(7), def); got != 7*24*time.Hour {
		t.Errorf("7 days = %v", got)
	}
}

func TestVolumePercentMeasuresARealPathAndRefusesAMissingOne(t *testing.T) {
	pct, ok := VolumePercent(t.TempDir())()
	if !ok || pct < 0 || pct > 100 {
		t.Fatalf("a temp dir is measurable: %v, %v", pct, ok)
	}
	if _, ok := VolumePercent("")(); ok {
		t.Fatal("an empty path must be unmeasurable")
	}
	if _, ok := VolumePercent("/definitely/not/here")(); ok {
		t.Fatal("a missing path must be unmeasurable, not fall back to another disk")
	}
}
