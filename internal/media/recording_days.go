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
		all, err = s.cachedDays(ctx, key, func() ([]string, error) { return s.nvrDays(ctx, cam, from, to, loc) })
		if err != nil {
			return nil, err
		}
		return daysWithin(all, from, to, loc), nil
	}
	key := "frigate|" + cam.Server.ID.String() + "|" + loc.String()
	all, err = s.cachedDays(ctx, key, func() ([]string, error) {
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
	return daysWithin(all, from, to, loc), nil
}

// daysWithin keeps the days of the list that fall between the local dates of from and to, so a recorder and a
// Frigate server answer for exactly the window that was asked.
func daysWithin(all []string, from, to time.Time, loc *time.Location) []string {
	first, last := from.In(loc).Format("2006-01-02"), to.In(loc).Format("2006-01-02")
	out := make([]string, 0, len(all))
	for _, d := range all {
		if d >= first && d <= last {
			out = append(out, d)
		}
	}
	return out
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
			// A stretch that ends exactly at midnight has no video on the day that starts there.
			seen[sp.End.Add(-time.Nanosecond).In(loc).Format("2006-01-02")] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

// maxRecordingDaysEntries bounds the cache: a caller may vary the window freely, so old entries are dropped.
const maxRecordingDaysEntries = 256

// recordingDaysFlight is one load in progress; callers that ask for the same key meanwhile wait for it.
type recordingDaysFlight struct {
	done chan struct{}
	days []string
	err  error
}

// cachedDays returns the days for key from the cache, or loads them. Concurrent loads of one key share a single
// call (Frigate takes seconds to answer), a failure is not remembered, and expired or surplus entries are dropped.
func (s *Service) cachedDays(ctx context.Context, key string, load func() ([]string, error)) ([]string, error) {
	s.daysMu.Lock()
	if e, ok := s.days[key]; ok && time.Now().Before(e.expires) {
		s.daysMu.Unlock()
		return e.days, nil
	}
	if f, ok := s.daysFlights[key]; ok {
		s.daysMu.Unlock()
		select {
		case <-f.done:
			return f.days, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f := &recordingDaysFlight{done: make(chan struct{})}
	if s.daysFlights == nil {
		s.daysFlights = map[string]*recordingDaysFlight{}
	}
	s.daysFlights[key] = f
	s.daysMu.Unlock()

	f.days, f.err = load()

	s.daysMu.Lock()
	delete(s.daysFlights, key)
	if f.err == nil {
		if s.days == nil {
			s.days = map[string]recordingDaysEntry{}
		}
		s.evictRecordingDays()
		s.days[key] = recordingDaysEntry{days: f.days, expires: time.Now().Add(recordingDaysTTL)}
	}
	s.daysMu.Unlock()
	close(f.done)
	return f.days, f.err
}

// evictRecordingDays drops expired entries and, when the cache is still full, the ones that expire first.
// The caller holds daysMu.
func (s *Service) evictRecordingDays() {
	now := time.Now()
	for k, e := range s.days {
		if !now.Before(e.expires) {
			delete(s.days, k)
		}
	}
	for len(s.days) >= maxRecordingDaysEntries {
		var oldest string
		var at time.Time
		for k, e := range s.days {
			if oldest == "" || e.expires.Before(at) {
				oldest, at = k, e.expires
			}
		}
		delete(s.days, oldest)
	}
}
