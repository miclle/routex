package config

import "testing"

func TestPrivateEgressPolicyIsIndependent(t *testing.T) {
	t.Setenv("ROUTEX_TEST_EGRESS", "true")
	config, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_egresses: \"${ROUTEX_TEST_EGRESS:-false}\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !config.AllowPrivateEgresses || config.AllowPrivateUpstreams {
		t.Fatal("proxy opt-in changed target policy")
	}
	config, err = Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_upstreams: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if config.AllowPrivateEgresses || !config.AllowPrivateUpstreams {
		t.Fatal("target opt-in changed proxy policy")
	}
	if _, err := Load(writeConfig(t, "addr: localhost:9000\ndsn: test\nallow_private_egresses: invalid\n")); err == nil {
		t.Fatal("malformed policy accepted")
	}
}
