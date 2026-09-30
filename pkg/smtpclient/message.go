package smtpclient

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Message struct{ From, Name, ReplyTo, To, RequestID, Subject, Body string }

// Send transmits one validated, caller-owned server message. Message content is
// bounded and header-safe; delivery stage results remain redacted.
func (c *Client) Send(ctx context.Context, config Config, message Message) Result {
	return c.run(ctx, config, &message)
}

func ValidAddress(value string) bool {
	if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Name != "" || parsed.Address != value || !strings.Contains(value, "@") {
		return false
	}
	for _, c := range value {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func ValidRequestID(value string) bool {
	if len(value) < 16 || len(value) > 80 {
		return false
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return false
		}
	}
	return true
}
func (m Message) Validate() error {
	if !ValidAddress(m.From) || !ValidAddress(m.To) || (m.ReplyTo != "" && !ValidAddress(m.ReplyTo)) || !ValidRequestID(m.RequestID) || !ValidSenderName(m.Name) {
		return errors.New("invalid SMTP test message")
	}
	if (m.Subject == "") != (m.Body == "") || len(m.Subject) > 120 || len(m.Body) > 4096 || !utf8.ValidString(m.Subject+m.Body) || strings.ContainsAny(m.Subject, "\r\n\x00") || strings.ContainsAny(m.Body, "\r\x00") {
		return errors.New("invalid SMTP message content")
	}
	for _, r := range m.Subject {
		if unicode.IsControl(r) {
			return errors.New("invalid SMTP message subject")
		}
	}
	return nil
}
func (m Message) text() string {
	from := (&mail.Address{Name: m.Name, Address: m.From}).String()
	subject, body := m.Subject, m.Body
	if subject == "" {
		subject = "RouteX SMTP test"
		body = "This is a RouteX SMTP configuration test.\nAcceptance by the configured relay does not confirm inbox delivery."
	}
	value := "From: " + from + "\r\nTo: " + m.To + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMessage-ID: <routex-" + m.RequestID + "@routex.invalid>\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 7bit\r\n"
	if m.ReplyTo != "" {
		value += "Reply-To: " + m.ReplyTo + "\r\n"
	}
	return value + "\r\n" + strings.ReplaceAll(body, "\n", "\r\n") + "\r\n"
}

func (Message) String() string { return "SMTP message (redacted)" }

func ValidSenderName(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 100 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
