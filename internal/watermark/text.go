// Package watermark burns the plate detail watermark (detection date/time with an explicit
// UTC offset, plus the configured owner name/logo) into downloaded photos (this package,
// PDW-3) and clips (ffmpeg drawtext/overlay in the worker, PDW-4). Text renders the exact
// same string in both places, and it must match apps/web/src/lib/format.ts's
// fmtWatermarkTimestamp exactly, since PDW-2's on-screen CSS overlay uses that function —
// the point of a watermark is that what you see is what gets burned in.
package watermark

import (
	"strings"
	"time"
)

// Text returns "<local date> <local time> <±HH:MM>[ · <owner name>]" for seenAt/ownerName,
// rendered in loc (PDW-7: the tenant's configured IANA time zone, resolved by
// branding.ResolveLocation — nil falls back to UTC). The numeric offset is always explicit
// (Go's "-07:00" layout verb), so the timestamp stays unambiguous like PDW-2's original
// UTC-only design, but now shows the real local time and its real offset — including DST,
// computed from loc's own transition rules, never hardcoded — instead of always UTC+00:00.
func Text(seenAt time.Time, ownerName string, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	ts := seenAt.In(loc).Format("2006-01-02 15:04:05 -07:00")
	ownerName = strings.TrimSpace(ownerName)
	if ownerName == "" {
		return ts
	}
	return ts + " · " + ownerName
}
