package vehicle

import "testing"

func TestEffectiveDefaultsOn(t *testing.T) {
	if !Effective(nil, nil) {
		t.Fatal("default is off")
	}
}

func TestEffectiveServerOffCoversTheCamera(t *testing.T) {
	off := false
	on := true
	if Effective(&off, &on) {
		t.Fatal("a disabled server still classified")
	}
}

func TestEffectiveCameraOff(t *testing.T) {
	on := true
	off := false
	if Effective(&on, &off) {
		t.Fatal("a disabled camera still classified")
	}
}
