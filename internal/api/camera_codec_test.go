package api

import (
	"testing"

	"github.com/jdolan-exalink/openvms/internal/store/db"
)

func TestToCameraExposesStreamCodecsOnlyWhenKnown(t *testing.T) {
	known := toCamera(db.GetCameraRow{LiveCodec: "h265", HqCodec: "h264"})
	if known.LiveCodec == nil || *known.LiveCodec != "h265" || known.HqCodec == nil || *known.HqCodec != "h264" {
		t.Fatalf("known codecs: live=%v hq=%v", known.LiveCodec, known.HqCodec)
	}
	unknown := toCamera(db.GetCameraRow{})
	if unknown.LiveCodec != nil || unknown.HqCodec != nil {
		t.Fatalf("unknown codecs must be null, got live=%v hq=%v", unknown.LiveCodec, unknown.HqCodec)
	}
}
