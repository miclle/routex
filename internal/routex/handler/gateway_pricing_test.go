package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failUsageWriter struct {
	http.ResponseWriter
	cancel context.CancelFunc
}

func (w failUsageWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("client disconnected")
}
func TestGatewayFinalUsageSurvivesClientDisconnect(t *testing.T) {
	final := `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}}}` + "\n\ndata: [DONE]\n\n"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	usage, err := proxyGatewayStream(ctx, failUsageWriter{httptest.NewRecorder(), cancel}, strings.NewReader(final), "public")
	if err == nil || !usage.Complete || usage.CacheWrite == nil || *usage.CacheWrite != 10 {
		t.Fatal("client disconnect discarded authoritative final usage")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	usage, err = proxyGatewayStream(canceled, httptest.NewRecorder(), strings.NewReader(final), "public")
	if err == nil || usage.Complete || usage.Input != nil {
		t.Fatal("cancellation before reading usage invented final counters")
	}
}
func TestGatewayUsageFramesNeverMerge(t *testing.T) {
	stream := `data: {"object":"chat.completion.chunk","choices":[{"finish_reason":null}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}}}` + "\n\n" + `data: {"object":"chat.completion.chunk","choices":[],"usage":{"completion_tokens":25}}` + "\n\ndata: [DONE]\n\n"
	usage, err := proxyGatewayStream(context.Background(), httptest.NewRecorder(), strings.NewReader(stream), "public")
	if err != nil || !usage.Complete || usage.Input != nil || usage.CacheRead != nil || usage.Output == nil || *usage.Output != 25 {
		t.Fatal("independent frames were merged into fabricated complete usage")
	}
}

func TestGatewayStreamRetainsUnsupportedTierFromEarlierChunk(t *testing.T) {
	stream := `data: {"object":"chat.completion.chunk","service_tier":"flex","choices":[{"finish_reason":null}],"usage":null}` + "\n\n" + `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}` + "\n\ndata: [DONE]\n\n"
	usage, err := proxyGatewayStream(context.Background(), httptest.NewRecorder(), strings.NewReader(stream), "public")
	if err != nil || !usage.Complete || !usage.Unsupported || len(usage.UnsupportedDimensions) != 1 || usage.UnsupportedDimensions[0] != "response_service_tier" {
		t.Fatal("final frame discarded earlier service-tier evidence")
	}
}
