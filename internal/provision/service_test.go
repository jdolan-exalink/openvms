package provision

import (
	"testing"

	"github.com/google/uuid"
)

func TestBackgroundRequestPreservesDemoStorageAuthorizationWithoutPassword(t *testing.T) {
	in := Request{
		SiteID:          uuid.New(),
		IP:              "192.0.2.14",
		User:            "root",
		Password:        "one-time-secret",
		ServerName:      "Cameras",
		HostKey:         "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		TrustOnFirstUse: true,
		AllowSystemDisk: true,
	}
	got := backgroundRequest(in)
	if !got.AllowSystemDisk {
		t.Fatal("system-disk demo authorization was dropped before the background runner")
	}
	if got.Password != "" {
		t.Fatal("SSH password must not be copied into the sanitized background request")
	}
	if got.SiteID != in.SiteID || got.IP != in.IP || got.ServerName != in.ServerName || got.User != in.User || got.HostKey != in.HostKey || got.TrustOnFirstUse != in.TrustOnFirstUse {
		t.Fatalf("sanitized request lost non-secret install fields: got=%#v", got)
	}
}

func TestValidateRequiresExplicitFirstContactTrustOrValidFingerprint(t *testing.T) {
	base := Request{SiteID: uuid.New(), IP: "192.0.2.14", ServerName: "Cameras", User: "root", Password: "secret"}
	if err := validate(base); err == nil {
		t.Fatal("missing fingerprint must require explicit first-contact trust")
	}
	base.TrustOnFirstUse = true
	if err := validate(base); err != nil {
		t.Fatalf("explicit first-contact trust should permit missing fingerprint: %v", err)
	}
	base.HostKey = "SHA256:invalid"
	if err := validate(base); err == nil {
		t.Fatal("a supplied invalid fingerprint must still be rejected when first-contact trust is enabled")
	}
	base.HostKey = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := validate(base); err != nil {
		t.Fatalf("valid supplied fingerprint should be accepted: %v", err)
	}
}
