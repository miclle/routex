package smtpclient

import (
	"strings"
	"testing"
)

func TestOperationalMessageIsBoundedAndHeaderSafe(t *testing.T) {
	message := Message{
		From: "sender@example.invalid", Name: "RouteX", To: "owner@example.invalid",
		RequestID: "notification_delivery_001", Subject: "RouteX operational alert: high",
		Body: "Kind: system_job_failure\nDetail: persistence_failed",
	}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
	text := message.text()
	if !strings.Contains(text, "Subject: RouteX operational alert: high") || !strings.Contains(text, "Kind: system_job_failure\r\n") {
		t.Fatalf("operational content missing: %q", text)
	}
	for _, mutation := range []Message{
		{From: message.From, Name: message.Name, To: message.To, RequestID: message.RequestID, Subject: "safe\r\nBcc: hidden@example.invalid", Body: "body"},
		{From: message.From, Name: message.Name, To: message.To, RequestID: message.RequestID, Subject: "safe", Body: "body\rspoof"},
		{From: message.From, Name: message.Name, To: message.To, RequestID: message.RequestID, Subject: "safe", Body: strings.Repeat("x", 4097)},
	} {
		if mutation.Validate() == nil {
			t.Fatal("unsafe operational message accepted")
		}
	}
}
