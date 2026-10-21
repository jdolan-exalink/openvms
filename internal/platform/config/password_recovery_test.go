package config

import "testing"

func TestPasswordRecoveryLogCodesDefaultsOnAndCanBeDisabled(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("PASSWORD_RECOVERY_LOG_CODES", "")
	c, err := Load("test")
	if err != nil {
		t.Fatal(err)
	}
	if !c.PasswordRecoveryLogCodes {
		t.Fatal("log-code recovery must be on by default")
	}
	t.Setenv("PASSWORD_RECOVERY_LOG_CODES", "false")
	if c, err = Load("test"); err != nil {
		t.Fatal(err)
	}
	if c.PasswordRecoveryLogCodes {
		t.Fatal("PASSWORD_RECOVERY_LOG_CODES=false must disable it")
	}
}
