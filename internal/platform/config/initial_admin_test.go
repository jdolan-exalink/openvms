package config

import "testing"

func TestInitialAdminDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OPENVMS_ADMIN_USERNAME", "")
	t.Setenv("OPENVMS_ADMIN_PASSWORD", "")
	c, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if c.AdminUsername != "admin" || c.AdminPassword != "openvms123" {
		t.Fatalf("defaults = %q/%q", c.AdminUsername, c.AdminPassword)
	}
}

func TestInitialAdminOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("OPENVMS_ADMIN_USERNAME", "root")
	t.Setenv("OPENVMS_ADMIN_PASSWORD", "Otra-Clave-2026")
	c, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if c.AdminUsername != "root" || c.AdminPassword != "Otra-Clave-2026" {
		t.Fatalf("got %q/%q", c.AdminUsername, c.AdminPassword)
	}
}
