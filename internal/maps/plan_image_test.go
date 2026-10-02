package maps

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestCanonicalPlanPNG(t *testing.T) {
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	hugeSide := append([]byte{}, original.Bytes()...)
	binary.BigEndian.PutUint32(hugeSide[16:20], MaxPlanSide+1)
	binary.BigEndian.PutUint32(hugeSide[29:33], crc32.ChecksumIEEE(hugeSide[12:29]))
	hugePixels := append([]byte{}, original.Bytes()...)
	binary.BigEndian.PutUint32(hugePixels[16:20], 4096)
	binary.BigEndian.PutUint32(hugePixels[20:24], 4096)
	binary.BigEndian.PutUint32(hugePixels[29:33], crc32.ChecksumIEEE(hugePixels[12:29]))
	cases := []struct {
		name string
		data []byte
		bad  bool
	}{
		{"valid", original.Bytes(), false},
		{"trailing active content", append(append([]byte{}, original.Bytes()...), []byte("<script>bad</script>")...), false},
		{"huge side", hugeSide, true},
		{"huge pixels", hugePixels, true},
		{"raw svg", []byte(`<svg onload="bad()"/>`), true},
		{"raw pdf", []byte("%PDF-1.7"), true},
		{"corrupt PNG body", original.Bytes()[:len(original.Bytes())-15], true},
		{"oversize upload", make([]byte, MaxPlanBytes+1), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, w, h, err := canonicalPlanPNG(tt.data)
			if tt.bad {
				if err == nil {
					t.Fatal("accepted invalid image")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if w != 4 || h != 3 {
				t.Fatalf("wrong size: %dx%d", w, h)
			}
			if !bytes.Equal(got, original.Bytes()) {
				t.Fatal("must reencode to canonical PNG without trailing bytes")
			}
		})
	}
}
