package media

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/jdolan-exalink/openvms/internal/authz"
	"github.com/jdolan-exalink/openvms/internal/inventory"
)

// recordingDaysTTL is how long a list of recording days is reused. A day with video appears within minutes
// anyway, and Frigate takes seconds to answer.
const recordingDaysTTL = 10 * time.Minute

// maxRecordingDaysRange bounds the window a calendar may ask about.
const maxRecordingDaysRange = 62 * 24 * time.Hour

type recordingDaysEntry struct {
	days    []string
	expires time.Time
}

// RecordingDays lists the days ("YYYY-MM-DD" in loc) between from and to on which the camera's server holds
// recordings, for the day picker. A Frigate server answers for all its cameras at once; a recorder is asked
// for the camera's own record log.
func (s *Service) RecordingDays(ctx context.Context, actor authz.Actor, cameraID uuid.UUID, from, to time.Time, loc *time.Location) ([]string, error) {
	if !to.After(from) || to.Sub(from) > maxRecordingDaysRange {
		return nil, invalid("the range must be positive and at most 62 days")
	}
	if loc == nil {
		loc = time.UTC
	}
	cam, err := s.Authorize(ctx, actor, cameraID, authz.RecordingsSeek)
	if err != nil {
		return nil, err
	}
	var all []string
	if cam.Server.Kind != "" && cam.Server.Kind != inventory.KindFrigate {
		if s.NVR == nil {
			return nil, &inventory.UnsupportedKindError{Kind: cam.Server.Kind}
		}
		key := "nvr|" + cam.ID.String() + "|" + loc.String() + "|" + from.UTC().Format(time.RFC3339) + "|" + to.UTC().Format(time.RFC3339)
		return s.cachedDays(key, func() ([]string, error) { return s.nvrDays(ctx, cam, from, to, loc) })
	}
	key := "frigate|" + cam.Server.ID.String() + "|" + loc.String()
	all, err = s.cachedDays(key, func() ([]string, error) {
		a, err := s.Adapters.Get(ctx, cam.Server)
		if err != nil {
			return nil, &inventory.FrigateError{Err: err}
		}
		days, err := a.RecordingDays(ctx, loc.String())
		if err != nil {
			return nil, &inventory.FrigateError{Err: err}
		}
		return days, nil
	})
	if err != nil {
		return nil, err
	}
	first, last := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	out := make([]string, 0, len(all))
	for _, d := range all {
		if d >= first && d <= last {
			out = append(out, d)
		}
	}
	return out, nil
}

// nvrDays reads the record log in one-week pieces (a month of motion segments is thousands of entries).
func (s *Service) nvrDays(ctx context.Context, cam Camera, from, to time.Time, loc *time.Location) ([]string, error) {
	seen := map[string]bool{}
	const step = 7 * 24 * time.Hour
	for start := from; start.Before(to); start = start.Add(step) {
		end := start.Add(step)
		if end.After(to) {
			end = to
		}
		spans, err := s.NVR.Recordings(ctx, cam, start, end)
		if err != nil {
			return nil, err
		}
		for _, sp := range spans {
			seen[sp.Start.In(loc).Format("2006-01-02")] = true
			seen[sp.End.In(loc).Format("2006-01-02")] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) cachedDays(key string, load func() ([]string, error)) ([]string, error) {
	s.daysMu.Lock()
	if e, ok := s.days[key]; ok && time.Now().Before(e.expires) {
		s.daysMu.Unlock()
		return e.days, nil
	}
	s.daysMu.Unlock()
	days, err := load()
	if err != nil {
		return nil, err
	}
	s.daysMu.Lock()
	if s.days == nil {
		s.days = map[string]recordingDaysEntry{}
	}
	s.days[key] = recordingDaysEntry{days: days, expires: time.Now().Add(recordingDaysTTL)}
	s.daysMu.Unlock()
	return days, nil
}
