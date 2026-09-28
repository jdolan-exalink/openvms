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

// Text returns "<UTC date> <UTC time> UTC+00:00[ · <owner name>]" for seenAt/ownerName. UTC
// with an explicit offset is always unambiguous, unlike a bare local time, and needs no
// extra data (PlateRead carries no time zone field); see the ODD doc (PDW-2) for the reasoning.
func Text(seenAt time.Time, ownerName string) string {
	ts := seenAt.UTC().Format("2006-01-02 15:04:05") + " UTC+00:00"
	ownerName = strings.TrimSpace(ownerName)
	if ownerName == "" {
		return ts
	}
	return ts + " · " + ownerName
}
