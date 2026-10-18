package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeDefaultBindingCannotLaunch(t *testing.T) {
	if validClaudeBinding() {
		t.Fatal("unbound build accepted")
	}
	var output bytes.Buffer
	if run([]string{"client-messages"}, &output) != 1 || !strings.Contains(output.String(), `"result":"client_binding_invalid"`) || strings.Contains(output.String(), `"original_joined":true`) {
		t.Fatal("unbound client proceeded")
	}
	output.Reset()
	if run([]string{"serve-messages"}, &output) != 1 || !strings.Contains(output.String(), `"result":"messages_config_invalid"`) {
		t.Fatal("unbound peer proceeded")
	}
}
func TestClaudeFixedArgsAndAllowlistedEnvironment(t *testing.T) {
	expected := []string{"--bare", "--print", "--tools", "", "--no-session-persistence", "--model", messagesModel, prompt}
	if !reflect.DeepEqual(claudeArgs(), expected) {
		t.Fatal("client operation widened")
	}
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "foreign")
	t.Setenv("HTTP_PROXY", "foreign")
	t.Setenv("CLAUDE_CONFIG_DIR", "foreign")
	env := claudeEnv("/owned/home", "/owned/tmp", []byte("installed-only"))
	if len(env) != 9 {
		t.Fatal("unexpected environment member")
	}
	for _, entry := range env {
		if strings.Contains(entry, "foreign") {
			t.Fatal("user environment inherited")
		}
	}
	joined := strings.Join(env, "\n")
	for _, expected := range []string{"ANTHROPIC_API_KEY=installed-only", "ANTHROPIC_BASE_URL=" + claudeOrigin, "HOME=/owned/home"} {
		if !strings.Contains(joined, expected) {
			t.Fatal("fixed client environment missing")
		}
	}
	if strings.Contains(strings.Join(claudeArgs(), " "), "installed-only") {
		t.Fatal("key in argv")
	}
}
func TestClaudeOutputBoundAndExactAnswer(t *testing.T) {
	var good claudeOutput
	_, _ = good.Write([]byte(answer + "\n"))
	if !good.matchesAnswer() {
		t.Fatal("fixed answer rejected")
	}
	good.erase()
	if !good.empty() {
		t.Fatal("output not cleared")
	}
	for _, text := range []string{"prefix " + answer, answer + " suffix", " " + answer} {
		var bad claudeOutput
		_, _ = bad.Write([]byte(text))
		if bad.matchesAnswer() {
			t.Fatal("foreign output accepted")
		}
	}
	var bounded claudeOutput
	if _, err := bounded.Write(bytes.Repeat([]byte{'x'}, maxClaudeOutput+1)); err == nil || !bounded.exceeded() || len(bounded.data) > maxClaudeOutput {
		t.Fatal("output bound lost")
	}
}
