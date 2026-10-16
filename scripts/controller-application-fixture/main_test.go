package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestArgumentErrorsDoNotEcho(t *testing.T) {
	const secret = "rx_NeverPrintThisSecretOrAnArgument"
	for _, args := range [][]string{nil, {secret}, {"invoke", secret}, {"--config", secret}} {
		var out bytes.Buffer
		if run(args, &out) != 1 {
			t.Fatal("invalid arguments succeeded")
		}
		if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), `"result":"arguments_invalid"`) {
			t.Fatal("unsafe argument result")
		}
	}
}
func TestServeConfigBounds(t *testing.T) {
	good := serveConfig{Listen: "127.0.0.1:19002", LifetimeSeconds: 900, MaxRequests: 64, FinishReasons: []string{"length", "stop", "stop"}}
	if !validServeConfig(good) {
		t.Fatal("valid config rejected")
	}
	for name, change := range map[string]func(*serveConfig){
		"wildcard": func(c *serveConfig) { c.Listen = "0.0.0.0:19002" }, "hostname": func(c *serveConfig) { c.Listen = "localhost:19002" }, "leading zero": func(c *serveConfig) { c.Listen = "127.0.0.1:019002" }, "privileged": func(c *serveConfig) { c.Listen = "127.0.0.1:80" }, "long lifetime": func(c *serveConfig) { c.LifetimeSeconds = 901 }, "zero lifetime": func(c *serveConfig) { c.LifetimeSeconds = 0 }, "unbounded requests": func(c *serveConfig) { c.MaxRequests = 65 }, "zero requests": func(c *serveConfig) { c.MaxRequests = 0 }, "bad native finish": func(c *serveConfig) { c.FinishReasons = []string{"tool_calls"} }, "too many calls": func(c *serveConfig) { c.FinishReasons = []string{"stop", "stop", "stop", "stop", "stop"} },
	} {
		t.Run(name, func(t *testing.T) {
			c := good
			change(&c)
			if validServeConfig(c) {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
func TestConfigStrictDecoding(t *testing.T) {
	for _, raw := range []string{`{"slot_id":"x","unknown":1}`, `{"slot_id":"x"}{}`, `{"slot_id":2}`, `null`} {
		var c invokeConfig
		if decodeExact([]byte(raw), &c) == nil && slotPattern.MatchString(c.SlotID) {
			t.Fatal("invalid input accepted")
		}
	}
	valid := "kds_01arz3ndektsv4rrffq69g5fav"
	if !slotPattern.MatchString(valid) {
		t.Fatal("valid slot rejected")
	}
	for _, s := range []string{valid + "/../x", "../" + valid, strings.ToUpper(valid), "kds_81arz3ndektsv4rrffq69g5fav", valid + "\n"} {
		if slotPattern.MatchString(s) {
			t.Fatal("unsafe slot accepted")
		}
	}
	key := []byte("rx_" + strings.Repeat("a", 43))
	if !validKey(key) || validKey(append(key, '\n')) || validKey([]byte("bad")) {
		t.Fatal("key grammar")
	}
}

func TestJSONRejectsDuplicateAndTrailingAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"slot_id":"a","slot_id":"b"}`, `{"x":{"finish_reason":"stop","finish_reason":"length"}}`, `{"x":1}{}`, `null`, `[]`, strings.Repeat(`{"x":`, 14) + `1` + strings.Repeat(`}`, 14)} {
		if validJSONObject([]byte(raw)) {
			t.Fatal("ambiguous or unbounded JSON accepted")
		}
	}
	if !validJSONObject([]byte(`{"a":[1,{"b":"c"}],"z":null}`)) {
		t.Fatal("bounded object rejected")
	}
}
