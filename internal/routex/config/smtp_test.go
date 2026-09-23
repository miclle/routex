package config

import "testing"

func TestSMTPBootstrapPolicyIsIndependent(t *testing.T) {
	t.Setenv("ROUTEX_TEST_SMTP_PRIVATE", "true")
	cfg, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_smtp: \"${ROUTEX_TEST_SMTP_PRIVATE:-false}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowPrivateSMTP || cfg.AllowPrivateEgresses || cfg.AllowPrivateUpstreams {
		t.Fatal("SMTP policy changed unrelated outbound policy")
	}
	cfg, err = Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_upstreams: true\nallow_private_egresses: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowPrivateSMTP {
		t.Fatal("SMTP private access inherited another opt-in")
	}
	if _, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_smtp: invalid\n")); err == nil {
		t.Fatal("invalid SMTP boolean accepted")
	}
}
