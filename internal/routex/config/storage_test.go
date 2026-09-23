package config

import "testing"

func TestStorageBootstrapPolicyIsIndependent(t *testing.T) {
	t.Setenv("ROUTEX_TEST_STORAGE_PRIVATE", "true")
	cfg, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_storage: \"${ROUTEX_TEST_STORAGE_PRIVATE:-false}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowPrivateStorage || cfg.AllowPrivateSMTP || cfg.AllowPrivateUpstreams || cfg.AllowPrivateEgresses {
		t.Fatal("storage policy changed unrelated outbound policy")
	}
	cfg, err = Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_upstreams: true\nallow_private_egresses: true\nallow_private_smtp: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AllowPrivateStorage {
		t.Fatal("storage inherited another private policy")
	}
	if _, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_storage: invalid\n")); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}
