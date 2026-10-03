package provision

import (
	"strings"
	"testing"
)

func TestChoose(t *testing.T) {
	cases := []struct {
		name string
		in   Facts
		want Variant
	}{
		{"coral wins over everything", Facts{Coral: true, NVIDIA: true, OpenVINO: true}, VariantCoral},
		{"nvidia over openvino", Facts{NVIDIA: true, OpenVINO: true}, VariantTensorRT},
		{"openvino alone", Facts{OpenVINO: true}, VariantOpenVINO},
		{"nothing is cpu", Facts{}, VariantCPU},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Choose(tc.in); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestParseFacts(t *testing.T) {
	got, err := ParseFacts("noise\ncoral=0 nvidia=1 openvino=1\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Coral || !got.NVIDIA || !got.OpenVINO {
		t.Fatalf("%#v", got)
	}
	if _, err := ParseFacts("coral=1"); err == nil {
		t.Fatal("expected an incomplete probe to fail")
	}
}

func TestScrub(t *testing.T) {
	secret := "session-secret"
	got := Scrub("failed near "+secret, secret)
	if strings.Contains(got, secret) || !strings.Contains(got, "[redacted]") {
		t.Fatalf("secret leaked: %q", got)
	}
}
