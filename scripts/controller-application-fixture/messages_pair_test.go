package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func messagesPairConfigFixture() string {
	return `{"operation":"serve-messages-pair","source_commit":"679d8eac311f952b785bc12beb093563ea617361","run_id":"1234567890abcdef1234567890abcdef","phase":"f29-claude-messages-v1","listen":"127.0.0.1:19003","lifetime_seconds":900,"max_requests":32}`
}
func TestMessagesPairClosedConfigAndCeilings(t *testing.T) {
	good := messagesPairConfigFixture()
	var config messagesPairServeConfig
	if !decodeMessagesPairServe([]byte(good), &config) {
		t.Fatal("valid pair input refused")
	}
	for _, bad := range []string{
		strings.Replace(good, `"operation":"serve-messages-pair"`, `"operation":"serve-messages"`, 1),
		strings.Replace(good, `"operation":"serve-messages-pair",`, "", 1),
		strings.Replace(good, `"phase":"f29-claude-messages-v1"`, `"phase":"foreign"`, 1),
		strings.Replace(good, `"max_requests":32`, `"max_requests":65`, 1),
		strings.Replace(good, `"max_requests":32`, `"max_requests":0`, 1),
		strings.Replace(good, `"max_requests":32`, `"max_requests":32,"max_calls":3`, 1),
		strings.Replace(good, `"lifetime_seconds":900`, `"lifetime_seconds":901`, 1),
		strings.Replace(good, `"listen":"127.0.0.1:19003"`, `"listen":"0.0.0.0:19003"`, 1),
		strings.Replace(good, `"run_id":"1234567890abcdef1234567890abcdef"`, `"run_id":null`, 1),
		strings.Replace(good, `"run_id":"1234567890abcdef1234567890abcdef"`, `"Run_ID":"1234567890abcdef1234567890abcdef"`, 1),
		strings.Replace(good, `"max_requests":32`, `"max_requests":32,"max_requests":32`, 1),
		good + "{}", strings.Repeat("x", 4097),
	} {
		if decodeMessagesPairServe([]byte(bad), &config) {
			t.Fatal("pair input widened")
		}
	}
}
func TestMessagesPairExactlyTwoStreamsAndThirdRejected(t *testing.T) {
	var log bytes.Buffer
	peer := newMessagesPairPeer([]byte("controlled-upstream-secret-1234"), 8, &log)
	if messagesPairComplete(peer) {
		t.Fatal("empty pair completed")
	}
	if messagesRecorder(peer, "GET", "http://localhost/v1/models?limit=1000", "").Code != 200 {
		t.Fatal("discovery refused")
	}
	for i := 1; i <= 2; i++ {
		response := messagesRecorder(peer, "POST", "http://localhost/v1/messages?beta=true", messagesTestRequest())
		if response.Code != 200 || response.Body.String() != strings.Join(messagesFrames(), "") || peer.calls != i || peer.completed != i {
			t.Fatal("pair stream/ordinal changed")
		}
		if messagesPairComplete(peer) != (i == 2) {
			t.Fatal("premature/missing pair completion")
		}
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 429 || peer.calls != 2 || peer.completed != 2 {
		t.Fatal("third native call admitted")
	}
	select {
	case <-peer.exhausted:
	default:
		t.Fatal("third call did not close budget")
	}
	decoder := json.NewDecoder(&log)
	ordinals := []int{}
	for {
		var event result
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("invalid status log")
		}
		if event.Result == "call" {
			ordinals = append(ordinals, event.Ordinal)
			if event.Native != "completed" {
				t.Fatal("pair completion log changed")
			}
		}
	}
	if len(ordinals) != 2 || ordinals[0] != 1 || ordinals[1] != 2 {
		t.Fatal("pair ordinal attribution lost")
	}
}
func TestMessagesPairDiscoveryDoesNotBypassTotalRequestBudget(t *testing.T) {
	peer := newMessagesPairPeer([]byte("controlled-upstream-secret-1234"), 2, io.Discard)
	for i := 0; i < 2; i++ {
		if messagesRecorder(peer, "GET", "http://localhost/v1/models?limit=1000", "").Code != 200 {
			t.Fatal("discovery rejected early")
		}
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 429 || peer.calls != 0 || messagesPairComplete(peer) {
		t.Fatal("discovery bypassed total request ceiling")
	}
}
func TestMessagesPairFailedWriteConsumesSlotWithoutCompletion(t *testing.T) {
	var log bytes.Buffer
	peer := newMessagesPairPeer([]byte("controlled-upstream-secret-1234"), 8, &log)
	request := httptest.NewRequest(http.MethodPost, "http://localhost/v1/messages", strings.NewReader(messagesTestRequest()))
	request.Header.Set("X-Api-Key", "controlled-upstream-secret-1234")
	request.Header.Set("Anthropic-Version", "2023-06-01")
	peer.ServeHTTP(&failingMessagesWriter{header: make(http.Header)}, request)
	if peer.calls != 1 || peer.completed != 0 || messagesPairComplete(peer) {
		t.Fatal("failed terminal became successful pair")
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 200 || peer.calls != 2 || peer.completed != 1 || messagesPairComplete(peer) {
		t.Fatal("uncertain slot silently replaced")
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 429 {
		t.Fatal("retry escaped two-slot bound")
	}
}
func TestMessagesPairAdditionPreservesSingleCallConstructor(t *testing.T) {
	peer := newMessagesPeer([]byte("controlled-upstream-secret-1234"), 8, io.Discard)
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 200 || messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 429 || peer.calls != 1 || peer.pair {
		t.Fatal("old one-call mode widened")
	}
}

func TestMessagesPairInvalidRequestDoesNotConsumeNativeSlot(t *testing.T) {
	peer := newMessagesPairPeer([]byte("controlled-upstream-secret-1234"), 8, io.Discard)
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", strings.Replace(messagesTestRequest(), prompt, "foreign", 1)).Code != 400 || peer.calls != 0 || peer.completed != 0 {
		t.Fatal("rejected body consumed native slot")
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages/count_tokens", "{}").Code != 404 || peer.calls != 0 {
		t.Fatal("optional unsupported endpoint consumed native slot")
	}
	if messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest()).Code != 200 || peer.calls != 1 || messagesPairComplete(peer) {
		t.Fatal("valid first native call lost after rejection")
	}
}
