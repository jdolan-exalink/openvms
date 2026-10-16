package rules_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/rules"
)

func TestConditionsMatchesEvent(t *testing.T) {
	cam1 := uuid.New()
	cam2 := uuid.New()

	c := rules.Conditions{
		CameraIDs:  []uuid.UUID{cam1},
		Severities: []string{"alert"},
		Labels:     []string{"person", "car"},
		Zones:      []string{"driveway"},
	}

	// Exact match
	ev := rules.EventContext{
		CameraID: cam1,
		Severity: "alert",
		Labels:   []string{"person"},
		Zones:    []string{"driveway"},
	}
	if !c.MatchesEvent(ev) {
		t.Fatal("expected event to match conditions")
	}

	// Different camera
	evWrongCam := ev
	evWrongCam.CameraID = cam2
	if c.MatchesEvent(evWrongCam) {
		t.Fatal("did not expect event with different camera to match")
	}

	// Different severity
	evWrongSev := ev
	evWrongSev.Severity = "detection"
	if c.MatchesEvent(evWrongSev) {
		t.Fatal("did not expect event with different severity to match")
	}

	// Different label
	evWrongLabel := ev
	evWrongLabel.Labels = []string{"dog"}
	if c.MatchesEvent(evWrongLabel) {
		t.Fatal("did not expect event with non-matching label to match")
	}

	// Different zone
	evWrongZone := ev
	evWrongZone.Zones = []string{"backyard"}
	if c.MatchesEvent(evWrongZone) {
		t.Fatal("did not expect event with non-matching zone to match")
	}

	// Empty conditions match any event
	empty := rules.Conditions{}
	if !empty.MatchesEvent(evWrongCam) {
		t.Fatal("expected empty conditions to match any event")
	}
}

func TestConditionsMatchesCameraOffline(t *testing.T) {
	cam1 := uuid.New()
	cam2 := uuid.New()

	c := rules.Conditions{
		CameraIDs:       []uuid.UUID{cam1},
		DurationSeconds: 300,
	}

	if !c.MatchesCameraOffline(cam1, uuid.Nil, 5*time.Minute) {
		t.Fatal("expected camera offline to match")
	}

	if c.MatchesCameraOffline(cam1, uuid.Nil, 4*time.Minute) {
		t.Fatal("expected camera offline below duration threshold to not match")
	}

	if c.MatchesCameraOffline(cam2, uuid.Nil, 10*time.Minute) {
		t.Fatal("expected different camera to not match")
	}
}

func TestConditionsMatchesServerOffline(t *testing.T) {
	srv1 := uuid.New()
	srv2 := uuid.New()

	c := rules.Conditions{
		ServerIDs:       []uuid.UUID{srv1},
		DurationSeconds: 120,
	}

	if !c.MatchesServerOffline(srv1, uuid.Nil, 2*time.Minute) {
		t.Fatal("expected server offline to match")
	}

	if c.MatchesServerOffline(srv1, uuid.Nil, 1*time.Minute) {
		t.Fatal("expected server offline below duration to not match")
	}

	if c.MatchesServerOffline(srv2, uuid.Nil, 5*time.Minute) {
		t.Fatal("expected different server to not match")
	}
}

func TestConditionsVehicleAttributes(t *testing.T) {
	c := rules.Conditions{VehicleTypes: []string{"pickup"}, VehicleColors: []string{"black"}}
	if c.MatchesEvent(rules.EventContext{Labels: []string{"car"}}) {
		t.Fatal("event without attributes must not match a vehicle rule")
	}
	if !c.MatchesEvent(rules.EventContext{VehicleType: "pickup", VehicleColor: "black"}) {
		t.Fatal("expected pickup black to match")
	}
	if c.MatchesEvent(rules.EventContext{VehicleType: "suv", VehicleColor: "black"}) {
		t.Fatal("did not expect suv to match a pickup rule")
	}
}

func TestConditionsSiteFilter(t *testing.T) {
	siteA, siteB := uuid.New(), uuid.New()
	c := rules.Conditions{SiteIDs: []uuid.UUID{siteA}}

	if !c.MatchesEvent(rules.EventContext{SiteID: siteA}) {
		t.Fatal("expected event in the selected site to match")
	}
	if c.MatchesEvent(rules.EventContext{SiteID: siteB}) {
		t.Fatal("did not expect event in another site to match")
	}
	if !c.MatchesCameraOffline(uuid.New(), siteA, time.Minute) {
		t.Fatal("expected camera in the selected site to match")
	}
	if c.MatchesCameraOffline(uuid.New(), siteB, time.Minute) {
		t.Fatal("did not expect camera in another site to match")
	}
	if !c.MatchesServerOffline(uuid.New(), siteA, time.Minute) {
		t.Fatal("expected server in the selected site to match")
	}
	if c.MatchesServerOffline(uuid.New(), siteB, time.Minute) {
		t.Fatal("did not expect server in another site to match")
	}

	empty := rules.Conditions{}
	if !empty.MatchesEvent(rules.EventContext{SiteID: siteB}) || !empty.MatchesCameraOffline(uuid.New(), siteB, 0) || !empty.MatchesServerOffline(uuid.New(), siteB, 0) {
		t.Fatal("expected empty site filter to match every site")
	}
}

func TestConditionsCameraGroupIDs(t *testing.T) {
	groupA := uuid.New()
	groupB := uuid.New()
	groupC := uuid.New()

	tests := []struct {
		name       string
		condGroups []uuid.UUID
		evGroups   []uuid.UUID
		want       bool
	}{
		{
			name:       "empty condition matches event without groups",
			condGroups: nil,
			evGroups:   nil,
			want:       true,
		},
		{
			name:       "matches when camera belongs to one of the rule groups",
			condGroups: []uuid.UUID{groupA, groupB},
			evGroups:   []uuid.UUID{groupB, groupC},
			want:       true,
		},
		{
			name:       "does not match when camera belongs to disjoint group",
			condGroups: []uuid.UUID{groupA},
			evGroups:   []uuid.UUID{groupB},
			want:       false,
		},
		{
			name:       "does not match when camera belongs to no groups",
			condGroups: []uuid.UUID{groupA},
			evGroups:   nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := rules.Conditions{CameraGroupIDs: tt.condGroups}
			ev := rules.EventContext{CameraGroupIDs: tt.evGroups}
			if got := c.MatchesEvent(ev); got != tt.want {
				t.Fatalf("MatchesEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionsSchedule(t *testing.T) {
	mustTime := func(value string) time.Time {
		t.Helper()
		ts, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}

	tests := []struct {
		name     string
		schedule *rules.RuleSchedule
		siteTZ   string
		start    time.Time
		want     bool
	}{
		{
			name:   "nil schedule matches any timestamp",
			siteTZ: "America/Argentina/Cordoba",
			start:  mustTime("2026-10-16T03:00:00Z"),
			want:   true,
		},
		{
			name: "daytime window in site timezone matches inside window",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5}, // Friday
				StartTime:  "08:00",
				EndTime:    "18:00",
			},
			siteTZ: "America/Argentina/Cordoba", // UTC-3: 15:00Z is 12:00 Friday in Cordoba
			start:  mustTime("2026-10-16T15:00:00Z"),
			want:   true,
		},
		{
			name: "daytime window in site timezone rejects when UTC is in range but local site time is before start",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5}, // Friday
				StartTime:  "08:00",
				EndTime:    "18:00",
			},
			siteTZ: "America/Argentina/Cordoba", // UTC-3: 09:30Z is 06:30 Friday in Cordoba
			start:  mustTime("2026-10-16T09:30:00Z"),
			want:   false,
		},
		{
			name: "overnight window matches evening portion on start day",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5}, // Friday night shift (Fri 22:00 -> Sat 06:00)
				StartTime:  "22:00",
				EndTime:    "06:00",
			},
			siteTZ: "America/Argentina/Cordoba", // 2026-10-17T01:30:00Z is Friday 2026-10-16 22:30 in Cordoba
			start:  mustTime("2026-10-17T01:30:00Z"),
			want:   true,
		},
		{
			name: "overnight window matches early-morning portion on following calendar day",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5}, // Friday night shift (Fri 22:00 -> Sat 06:00)
				StartTime:  "22:00",
				EndTime:    "06:00",
			},
			siteTZ: "America/Argentina/Cordoba", // 2026-10-17T05:15:00Z is Saturday 2026-10-17 02:15 in Cordoba
			start:  mustTime("2026-10-17T05:15:00Z"),
			want:   true,
		},
		{
			name: "overnight window rejects early-morning portion when previous day is not in DaysOfWeek",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5}, // Friday night shift only
				StartTime:  "22:00",
				EndTime:    "06:00",
			},
			siteTZ: "America/Argentina/Cordoba", // 2026-10-16T05:15:00Z is Friday 2026-10-16 02:15 (Thursday night shift)
			start:  mustTime("2026-10-16T05:15:00Z"),
			want:   false,
		},
		{
			name: "overnight window rejects exact end boundary 06:00",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{5},
				StartTime:  "22:00",
				EndTime:    "06:00",
			},
			siteTZ: "America/Argentina/Cordoba", // Saturday 06:00 local
			start:  mustTime("2026-10-17T09:00:00Z"),
			want:   false,
		},
		{
			name: "DST spring-forward transition in America/New_York shifts UTC offset from -5 to -4",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{0}, // Sunday 2026-03-08
				StartTime:  "03:00",
				EndTime:    "05:00",
			},
			siteTZ: "America/New_York", // 07:30Z on 2026-03-08 is 03:30 EDT (after 02:00->03:00 jump)
			start:  mustTime("2026-03-08T07:30:00Z"),
			want:   true,
		},
		{
			name: "DST spring-forward overnight Saturday shift matches Sunday morning in EDT",
			schedule: &rules.RuleSchedule{
				DaysOfWeek: []int{6}, // Saturday 2026-03-07 22:00 EST -> Sunday 2026-03-08 06:00 EDT
				StartTime:  "22:00",
				EndTime:    "06:00",
			},
			siteTZ: "America/New_York", // 09:30Z on 2026-03-08 is 05:30 EDT
			start:  mustTime("2026-03-08T09:30:00Z"),
			want:   true,
		},
		{
			name: "falls back to Schedule.Timezone when SiteTimezone is empty, then UTC",
			schedule: &rules.RuleSchedule{
				StartTime: "20:00",
				EndTime:   "23:00",
				Timezone:  "America/Argentina/Cordoba",
			},
			siteTZ: "", // fallback to Schedule.Timezone (UTC-3: 23:30Z is 20:30 local)
			start:  mustTime("2026-10-16T23:30:00Z"),
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := rules.Conditions{Schedule: tt.schedule}
			ev := rules.EventContext{SiteTimezone: tt.siteTZ, Start: tt.start}
			if got := c.MatchesEvent(ev); got != tt.want {
				t.Fatalf("MatchesEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}
