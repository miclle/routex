package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestControllerCLIRejectsSecretFlagsWithoutEcho(t *testing.T) {
	for _, args := range [][]string{
		{"deliver", "-bearer", "argument-secret-canary"},
		{"deliver", "-config", "/missing/config.json", "argument-secret-canary"},
		{"unknown-secret-canary"},
		{"deliver"},
	} {
		var output bytes.Buffer
		err := run(context.Background(), args, &output)
		if err == nil {
			t.Fatal("invalid command accepted")
		}
		if strings.Contains(err.Error()+output.String(), "secret-canary") {
			t.Fatal("command diagnostic echoed supplied argument")
		}
	}
}

func TestControllerCLIHelpNeedsNoRuntime(t *testing.T) {
	for _, command := range []string{"deliver", "retry-report"} {
		var output bytes.Buffer
		if err := run(context.Background(), []string{command, "-help"}, &output); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "-config /absolute/controller.json") || strings.Contains(output.String(), "-bearer") {
			t.Fatal("unexpected help contract")
		}
	}
}
