package ldap

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	ber "github.com/go-asn1-ber/asn1-ber"
)

type fixtureConn struct{ bytes.Buffer }

func (c *fixtureConn) Close() error                     { return nil }
func (c *fixtureConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *fixtureConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *fixtureConn) SetDeadline(time.Time) error      { return nil }
func (c *fixtureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fixtureConn) SetWriteDeadline(time.Time) error { return nil }

func encodedEnvelope(id int64, tag ber.Tag, children ...*ber.Packet) []byte {
	packet := ber.NewSequence("")
	packet.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
	op := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
	for _, child := range children {
		op.AppendChild(child)
	}
	packet.AppendChild(op)
	return packet.Bytes()
}
func encodedResult(id int64, tag ber.Tag, code int64) []byte {
	return encodedEnvelope(id, tag, ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, ""), ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""), ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "private server diagnostic", ""))
}

func TestLDAPBERAdmissionBeforeParser(t *testing.T) {
	nested := []byte{0x04, 0}
	for range depthLimit {
		packet := ber.NewSequence("")
		child, err := ber.DecodePacketErr(nested)
		if err != nil {
			t.Fatal(err)
		}
		packet.AppendChild(child)
		nested = packet.Bytes()
	}
	crowded := ber.NewSequence("")
	for range nodeLimit {
		crowded.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	}
	for name, data := range map[string][]byte{"indefinite": {0x30, 0x80, 0, 0}, "nested indefinite": {0x30, 2, 0x30, 0x80}, "high tag": {0x3f, 0}, "truncated": {0x30, 5, 0x04, 1, 0}, "trailing": {0x30, 0, 0}, "deep": nested, "nodes": crowded.Bytes(), "empty integer": {0x02, 0}, "oversized integer": append([]byte{0x02, 9}, make([]byte, 9)...)} {
		t.Run(name, func(t *testing.T) {
			if validBER(data) {
				t.Fatal("unbounded or malformed frame admitted")
			}
		})
	}
	if !validBER(encodedResult(1, 1, 0)) {
		t.Fatal("finite result rejected")
	}
	for name, data := range map[string][]byte{"oversized header": {0x30, 0x83, 1, 0, 0}, "indefinite header": {0x30, 0x80}, "long length": {0x30, 0x84, 0, 0, 0, 1}, "wrong envelope": {0x31, 0}, "truncated body": {0x30, 2, 0}} {
		t.Run(name, func(t *testing.T) {
			if _, err := readFrame(bytes.NewReader(data), incomingLimit); err == nil {
				t.Fatal("frame header admitted")
			}
		})
	}
}

func TestLDAPBERCorrelationAndCompleteSequence(t *testing.T) {
	cases := []struct {
		name      string
		id        int64
		operation ber.Tag
		response  []byte
		want      bool
	}{
		{"bind", 1, 0, encodedResult(1, 1, 0), true},
		{"wrong id", 1, 0, encodedResult(2, 1, 0), false},
		{"wrong bind operation", 1, 0, encodedResult(1, 5, 0), false},
		{"search done", 2, 3, encodedResult(2, 5, 0), true},
		{"referral", 2, 3, encodedEnvelope(2, 19, ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "ldaps://other:636", "")), false},
		{"unexpected search operation", 2, 3, encodedResult(2, 1, 0), false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			c := boundedConn{messageID: test.id, operation: test.operation}
			if got := c.response(test.response); got != test.want {
				t.Fatal("correlation mismatch")
			}
			if test.want && c.response(test.response) {
				t.Fatal("duplicate terminal admitted")
			}
		})
	}
	controlled, _ := ber.DecodePacketErr(encodedResult(1, 1, 0))
	controlled.AppendChild(ber.Encode(ber.ClassContext, ber.TypeConstructed, 0, nil, ""))
	c := boundedConn{messageID: 1, operation: 0}
	if c.response(controlled.Bytes()) {
		t.Fatal("response controls admitted")
	}
}

func TestLDAPAggregateAndFrameCountBounds(t *testing.T) {
	data := encodedResult(1, 1, 0)
	raw := &fixtureConn{}
	_, _ = raw.Write(data)
	c := boundedConn{Conn: raw, messageID: 1, operation: 0, received: incomingLimit - len(data) + 1}
	if _, err := c.Read(make([]byte, 1)); err != ErrUnavailable {
		t.Fatal("aggregate incoming bound not enforced")
	}
	c = boundedConn{Conn: raw, messageID: 1, operation: 0, frames: frameCountLimit}
	if _, err := c.Read(make([]byte, 1)); err != ErrUnavailable {
		t.Fatal("frame count bound not enforced")
	}
	raw = &fixtureConn{}
	c = boundedConn{Conn: raw, sent: outgoingLimit - 1}
	if _, err := c.Write(encodedEnvelope(1, 0)); err != ErrUnavailable || raw.Len() != 0 {
		t.Fatal("outgoing bound dispatched data")
	}
	raw = &fixtureConn{}
	_, _ = raw.Write(data)
	c = boundedConn{Conn: raw, messageID: 1, operation: 0}
	result := make([]byte, len(data))
	if _, err := io.ReadFull(&c, result); err != nil || !bytes.Equal(result, data) {
		t.Fatal("bounded frame changed bytes")
	}
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("incomplete peer accepted")
	}
}

func TestLDAPWholeWireBoundsIncludeHandshakeBytes(t *testing.T) {
	raw := &fixtureConn{}
	_, _ = raw.Write(bytes.Repeat([]byte{1}, incomingLimit+1))
	wire := &wireConn{Conn: raw}
	received, err := io.Copy(io.Discard, wire)
	if received != incomingLimit || err != ErrUnavailable || raw.Len() != 1 {
		t.Fatal("incoming wire budget was exceeded", received, err)
	}
	raw = &fixtureConn{}
	wire = &wireConn{Conn: raw}
	if _, err := wire.Write(make([]byte, outgoingLimit)); err != nil {
		t.Fatal(err)
	}
	if _, err := wire.Write([]byte{1}); err != ErrUnavailable || raw.Len() != outgoingLimit {
		t.Fatal("outgoing wire budget was exceeded")
	}
}

func TestLDAPSearchEntryMandatoryChildShapes(t *testing.T) {
	dn := func() *ber.Packet {
		return ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "uid=member,dc=example", "")
	}
	attributes := func() *ber.Packet { return ber.NewSequence("") }
	cases := []struct {
		name     string
		children []*ber.Packet
		want     bool
	}{
		{"valid empty attributes", []*ber.Packet{dn(), attributes()}, true},
		{"octet string attributes", []*ber.Packet{dn(), ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "")}, false},
		{"primitive sequence attributes", []*ber.Packet{dn(), ber.Encode(ber.ClassUniversal, ber.TypePrimitive, ber.TagSequence, nil, "")}, false},
		{"context sequence attributes", []*ber.Packet{dn(), ber.Encode(ber.ClassContext, ber.TypeConstructed, ber.TagSequence, nil, "")}, false},
		{"UTF8String DN", []*ber.Packet{ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagUTF8String, "uid=member,dc=example", ""), attributes()}, false},
		{"constructed DN", []*ber.Packet{ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagOctetString, nil, ""), attributes()}, false},
		{"context DN", []*ber.Packet{ber.NewString(ber.ClassContext, ber.TypePrimitive, ber.TagOctetString, "uid=member,dc=example", ""), attributes()}, false},
		{"missing attributes", []*ber.Packet{dn()}, false},
		{"extra child", []*ber.Packet{dn(), attributes(), attributes()}, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			connection := boundedConn{messageID: 2, operation: 3}
			if got := connection.response(encodedEnvelope(2, 4, test.children...)); got != test.want {
				t.Fatal("mandatory search entry child shape mismatch")
			}
			if test.want && !connection.response(encodedResult(2, 5, 0)) {
				t.Fatal("valid entry did not retain completed-search admission")
			}
		})
	}
}

func TestLDAPStableAttributeMandatoryShapes(t *testing.T) {
	octet := func(value string) *ber.Packet {
		return ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, value, "")
	}
	validValues := func() *ber.Packet {
		set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
		set.AppendChild(octet("12345678-9abc-4def-8123-123456789abc"))
		return set
	}
	cases := []struct {
		name             string
		class            ber.Class
		kind             ber.Type
		tag              ber.Tag
		nameNode, values *ber.Packet
		want             bool
	}{
		{"valid", ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, octet("entryUUID"), validValues(), true},
		{"attribute context class", ber.ClassContext, ber.TypeConstructed, ber.TagSequence, octet("entryUUID"), validValues(), false},
		{"attribute primitive", ber.ClassUniversal, ber.TypePrimitive, ber.TagSequence, octet("entryUUID"), validValues(), false},
		{"name UTF8String", ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagUTF8String, "entryUUID", ""), validValues(), false},
		{"values sequence", ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, octet("entryUUID"), ber.NewSequence(""), false},
		{"values primitive SET", ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, octet("entryUUID"), ber.Encode(ber.ClassUniversal, ber.TypePrimitive, ber.TagSet, nil, ""), false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			attribute := ber.Encode(test.class, test.kind, test.tag, nil, "")
			attribute.AppendChild(test.nameNode)
			attribute.AppendChild(test.values)
			attrs := ber.NewSequence("")
			attrs.AppendChild(attribute)
			connection := boundedConn{messageID: 2, operation: 3}
			if got := connection.response(encodedEnvelope(2, 4, octet("uid=member,dc=example"), attrs)); got != test.want {
				t.Fatal("stable attribute wrapper shape mismatch")
			}
		})
	}
	set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
	set.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagUTF8String, "12345678-9abc-4def-8123-123456789abc", ""))
	attribute := ber.NewSequence("")
	attribute.AppendChild(octet("entryUUID"))
	attribute.AppendChild(set)
	attrs := ber.NewSequence("")
	attrs.AppendChild(attribute)
	connection := boundedConn{messageID: 2, operation: 3}
	if connection.response(encodedEnvelope(2, 4, octet("uid=member,dc=example"), attrs)) {
		t.Fatal("non-OCTET identity value admitted")
	}
}
