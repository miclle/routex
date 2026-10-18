package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"sync"
)

// Set only by a reviewed fresh build recipe (-X). Empty defaults are intentional.
// The version is provenance bound to the official digest, not a self-reported
// --version command (which would execute the artifact before verification).
var claudeSourceCommit, claudeRunID, claudePhase, claudeUID string
var claudeVersion, claudeSHA256, claudeSize, claudePlatform string

const claudeOrigin = "http://127.0.0.1:19001"
const claudePhaseName = "f29-claude-messages-v1"
const maxClaudeOutput = 64 << 10

var sourcePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var runPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var versionPattern = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

func validClaudeBinding() bool {
	uid, err := strconv.Atoi(claudeUID)
	size, sizeErr := strconv.ParseInt(claudeSize, 10, 64)
	return runtime.GOOS == "linux" && runtime.GOARCH == "arm64" && sourcePattern.MatchString(claudeSourceCommit) && claudeSourceCommit != "0000000000000000000000000000000000000000" && runPattern.MatchString(claudeRunID) && claudeRunID != "00000000000000000000000000000000" && claudePhase == claudePhaseName && err == nil && uid > 0 && strconv.Itoa(uid) == claudeUID && uid == os.Getuid() && uid == os.Geteuid() && versionPattern.MatchString(claudeVersion) && hashPattern.MatchString(claudeSHA256) && claudeSHA256 != "0000000000000000000000000000000000000000000000000000000000000000" && sizeErr == nil && size > 0 && size <= 512<<20 && strconv.FormatInt(size, 10) == claudeSize && (claudePlatform == "linux-arm64" || claudePlatform == "linux-arm64-musl")
}
func claudeArgs() []string {
	return []string{"--bare", "--print", "--tools", "", "--no-session-persistence", "--model", messagesModel, prompt}
}
func claudeEnv(home, tmp string, key []byte) []string {
	// Never append os.Environ: no user credentials, proxy, hook or settings values.
	return []string{"HOME=" + home, "TMPDIR=" + tmp, "XDG_CONFIG_HOME=" + home + "/config", "XDG_CACHE_HOME=" + home + "/cache", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "ANTHROPIC_API_KEY=" + string(key), "ANTHROPIC_BASE_URL=" + claudeOrigin, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1"}
}

type claudeResult struct {
	Result                   string `json:"result"`
	Native                   string `json:"native"`
	OriginalPID              int    `json:"original_pid,omitempty"`
	OriginalPGID             int    `json:"original_pgid,omitempty"`
	OriginalJoined           bool   `json:"original_joined"`
	OriginalExit             *int   `json:"original_exit"`
	GroupClosed              bool   `json:"group_closed"`
	OutputMatched            bool   `json:"output_matched"`
	ClientRequestIDDisplayed bool   `json:"client_request_id_displayed"`
	SourceCommit             string `json:"source_commit,omitempty"`
	RunID                    string `json:"run_id,omitempty"`
	Phase                    string `json:"phase,omitempty"`
	ClientVersion            string `json:"client_version,omitempty"`
}

func runClientMessages(out io.Writer) int { return launchClaude(out) }

// A short writer causes exec's copy goroutine to stop, without retaining output
// beyond the fixed bound. The supervisor notices overflow and ends its own group.
type claudeOutput struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *claudeOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > maxClaudeOutput-len(b.data) {
		b.overflow = true
		return 0, io.ErrShortWrite
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *claudeOutput) exceeded() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.overflow }
func (b *claudeOutput) matchesAnswer() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.overflow && bytes.Equal(bytes.TrimRight(b.data, "\r\n"), []byte(answer))
}
func (b *claudeOutput) empty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.overflow && len(b.data) == 0
}
func (b *claudeOutput) erase() { b.mu.Lock(); defer b.mu.Unlock(); clear(b.data); b.data = nil }
