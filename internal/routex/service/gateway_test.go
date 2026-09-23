package service

import (
	"encoding/json"
	"testing"
)

func TestParseGatewayChat(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"model":"m","messages":[]}`, `{"model":"m","messages":{}}`, `{"model":"m","messages":[{}],"stream":"yes"}`, `{"model":"m","messages":[{}],"stream":null}`, `{"model":"bad name","messages":[{}]}`} {
		if _, _, _, err := parseGatewayChat([]byte(raw)); err == nil {
			t.Errorf("invalid request accepted: %s", raw)
		}
	}
	payload, model, stream, err := parseGatewayChat([]byte(`{"model":"m","messages":[{"role":"user","content":"hello"}],"stream":true,"temperature":0.125,"tools":[{"type":"function"}]}`))
	if err != nil || model != "m" || !stream {
		t.Fatalf("valid request: %v", err)
	}
	if !json.Valid(payload["tools"]) || string(payload["temperature"]) != "0.125" {
		t.Fatal("native parameters changed")
	}
}

func TestGatewayWeightSelection(t *testing.T) {
	for _, weights := range [][]int{nil, {0}, {50}, {101}, {-1, 101}, {50, 51}} {
		if _, err := chooseGatewayRoute(weights); err == nil {
			t.Fatalf("invalid weights accepted: %v", weights)
		}
	}
	for range 100 {
		if selected, err := chooseGatewayRoute([]int{0, 100, 0}); err != nil || selected != 1 {
			t.Fatal("zero-weight candidate selected")
		}
	}
	seen := [2]bool{}
	for range 1000 {
		selected, err := chooseGatewayRoute([]int{50, 50})
		if err != nil {
			t.Fatal(err)
		}
		seen[selected] = true
	}
	if !seen[0] || !seen[1] {
		t.Fatal("weighted candidates were not both selected")
	}
}
