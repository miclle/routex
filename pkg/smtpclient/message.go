package smtpclient

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Message struct{ From, Name, ReplyTo, To, RequestID string }

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
	return nil
}
func (m Message) text() string {
	from := (&mail.Address{Name: m.Name, Address: m.From}).String()
	value := "From: " + from + "\r\nTo: " + m.To + "\r\nDate: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\nMessage-ID: <routex-" + m.RequestID + "@routex.invalid>\r\nSubject: RouteX SMTP test\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 7bit\r\n"
	if m.ReplyTo != "" {
		value += "Reply-To: " + m.ReplyTo + "\r\n"
	}
	return value + "\r\nThis is a RouteX SMTP configuration test.\r\nAcceptance by the configured relay does not confirm inbox delivery.\r\n"
}

func (Message) String() string { return "SMTP test message (redacted)" }

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
