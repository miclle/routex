// Command vault-certificate-fixture verifies saved identities only against the
// fresh loopback Vault and RouteX processes owned by its acceptance coordinator.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var BuildRunID, BuildSource, BuildSourceManifest, BuildHostUID, BuildStarted string

type nativeArtifact struct {
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func artifacts(dir string) ([]nativeArtifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 512 {
		return nil, errFixture
	}
	var rows []nativeArtifact
	total := 0
	for _, entry := range entries {
		name := entry.Name()
		if (!strings.HasPrefix(name, "before-") && !strings.HasPrefix(name, "rotate-") && !strings.HasPrefix(name, "after-")) || (filepath.Ext(name) != ".json" && filepath.Ext(name) != ".jsonl") {
			return nil, errFixture
		}
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 4<<20 {
			return nil, errFixture
		}
		// A captured HTTP 204 has an intentionally empty body. Missing files and
		// empty identity/configuration inputs are still refused separately.
		raw, err := os.ReadFile(path)
		if err != nil || int64(len(raw)) != info.Size() {
			return nil, errFixture
		}
		total += len(raw)
		if total > 32<<20 {
			return nil, errFixture
		}
		rows = append(rows, nativeArtifact{name, len(raw), checksum(raw)})
	}
	return rows, nil
}

func run(args []string) int {
	uid, err := strconv.Atoi(BuildHostUID)
	started, timeErr := time.Parse(time.RFC3339Nano, BuildStarted)
	if err != nil || uid <= 0 || uid != os.Getuid() || uid != os.Geteuid() || !runIDPattern.MatchString(BuildRunID) || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(BuildSource) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(BuildSourceManifest) || timeErr != nil || time.Since(started) < 0 || time.Since(started) > 30*time.Minute {
		return 1
	}
	if len(args) == 2 && args[0] == "prepare" && (runtime.GOOS == "darwin" || runtime.GOOS == "linux") {
		if preparePKI(args[1], BuildRunID, time.Now().UTC()) != nil {
			return 1
		}
		return 0
	}
	if len(args) != 1 || runtime.GOOS != "linux" || (args[0] != "before" && args[0] != "rotate" && args[0] != "after") {
		return 1
	}
	phase := args[0]
	dir := "/vault/logs/certificate-" + BuildRunID
	if phase == "before" && os.Mkdir(dir, 0700) != nil {
		return 1
	}
	budget := 120 * time.Second
	if phase == "rotate" {
		budget = 390 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	o := newFixtureOwner(ctx, dir, phase)
	defer o.client.CloseIdleConnections()
	var stageErr error
	switch phase {
	case "before":
		stageErr = o.before()
	case "rotate":
		stageErr = o.rotate()
	case "after":
		stageErr = o.after()
	}
	if o.save("http-census.json", o.facts) != nil {
		stageErr = errFixture
	}
	files, manifestErr := artifacts(dir)
	if manifestErr != nil {
		stageErr = errFixture
	}
	raw, marshalErr := json.Marshal(map[string]any{"run": BuildRunID, "source": BuildSource, "source_manifest": BuildSourceManifest, "phase": phase, "last_stage": o.stage, "passed": stageErr == nil, "files": files, "application_adoption": false, "native_AI_completion": false})
	if marshalErr != nil || writePrivate(filepath.Join(dir, phase+"-terminal.json"), raw) != nil || stageErr != nil {
		return 1
	}
	return 0
}

func main() {
	code := run(os.Args[1:])
	if code != 0 {
		fmt.Fprintln(os.Stderr, "certificate fixture refused")
	}
	os.Exit(code)
}
