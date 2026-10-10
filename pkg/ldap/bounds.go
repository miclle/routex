package ldap

import (
	"io"
	"net"
	"sync"

	ber "github.com/go-asn1-ber/asn1-ber"
)

const frameLimit = 64 * 1024
const incomingLimit = 256 * 1024
const outgoingLimit = 32 * 1024
const frameCountLimit = 16
const depthLimit = 32
const nodeLimit = 4096

// wireConn counts the entire operation, including TLS handshake records. Read
// and Write have distinct counters; each direction has one protocol owner.
type wireConn struct {
	net.Conn
	received int
	sent     int
}

func (c *wireConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	remaining := incomingLimit - c.received
	if remaining <= 0 {
		return 0, ErrUnavailable
	}
	if len(dst) > remaining {
		dst = dst[:remaining]
	}
	n, err := c.Conn.Read(dst)
	c.received += n
	return n, err
}

func (c *wireConn) Write(data []byte) (int, error) {
	if len(data) > outgoingLimit-c.sent {
		return 0, ErrUnavailable
	}
	n, err := c.Conn.Write(data)
	c.sent += n
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

// boundedConn admits one complete finite BER frame before the maintained parser
// sees any of it. These checks bound framing; LDAP decoding remains in go-ldap.
type boundedConn struct {
	net.Conn
	pending   []byte
	received  int
	sent      int
	frames    int
	mu        sync.Mutex
	messageID int64
	operation ber.Tag
	terminal  bool
}

func (c *boundedConn) Read(dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	if len(c.pending) == 0 {
		if c.frames >= frameCountLimit {
			return 0, ErrUnavailable
		}
		frame, err := readFrame(c.Conn, incomingLimit-c.received)
		if err != nil {
			return 0, ErrUnavailable
		}
		c.received += len(frame)
		c.frames++
		if !validBER(frame) || !c.response(frame) {
			return 0, ErrUnavailable
		}
		c.pending = frame
	}
	n := copy(dst, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *boundedConn) Write(data []byte) (int, error) {
	if len(data) > outgoingLimit-c.sent || !validBER(data) {
		return 0, ErrUnavailable
	}
	packet, err := ber.DecodePacketErr(data)
	if err != nil || len(packet.Children) != 2 {
		return 0, ErrUnavailable
	}
	id, ok := packet.Children[0].Value.(int64)
	op := packet.Children[1]
	if !ok || id < 1 || op.ClassType != ber.ClassApplication || op.TagType != ber.TypeConstructed || (op.Tag != 0 && op.Tag != 3) {
		return 0, ErrUnavailable
	}
	c.mu.Lock()
	c.messageID, c.operation, c.terminal = id, op.Tag, false
	c.mu.Unlock()
	n, err := c.Conn.Write(data)
	c.sent += n
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (c *boundedConn) response(data []byte) bool {
	packet, err := ber.DecodePacketErr(data)
	if err != nil || packet.ClassType != ber.ClassUniversal || packet.TagType != ber.TypeConstructed || packet.Tag != ber.TagSequence || len(packet.Children) != 2 {
		return false
	}
	idNode, op := packet.Children[0], packet.Children[1]
	id, ok := idNode.Value.(int64)
	if !ok || idNode.ClassType != ber.ClassUniversal || idNode.Tag != ber.TagInteger || idNode.TagType != ber.TypePrimitive || op.ClassType != ber.ClassApplication || op.TagType != ber.TypeConstructed {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if id != c.messageID || c.terminal {
		return false
	}
	switch c.operation {
	case 0:
		if op.Tag != 1 || !resultShape(op) {
			return false
		}
		c.terminal = true
	case 3:
		if op.Tag == 4 {
			return entryShape(op)
		}
		if op.Tag != 5 || !resultShape(op) {
			return false
		}
		c.terminal = true
	default:
		return false
	}
	return true
}

// Stable identity values require the mandatory LDAP attribute wrappers before
// maintained decoding. This validates shapes only, not attribute semantics.
func entryShape(op *ber.Packet) bool {
	if len(op.Children) != 2 {
		return false
	}
	dn, attributes := op.Children[0], op.Children[1]
	if !octetShape(dn) || attributes.ClassType != ber.ClassUniversal || attributes.TagType != ber.TypeConstructed || attributes.Tag != ber.TagSequence {
		return false
	}
	for _, attribute := range attributes.Children {
		if attribute.ClassType != ber.ClassUniversal || attribute.TagType != ber.TypeConstructed || attribute.Tag != ber.TagSequence || len(attribute.Children) != 2 || !octetShape(attribute.Children[0]) {
			return false
		}
		values := attribute.Children[1]
		if values.ClassType != ber.ClassUniversal || values.TagType != ber.TypeConstructed || values.Tag != ber.TagSet {
			return false
		}
		for _, value := range values.Children {
			if !octetShape(value) {
				return false
			}
		}
	}
	return true
}

func octetShape(packet *ber.Packet) bool {
	return packet.ClassType == ber.ClassUniversal && packet.TagType == ber.TypePrimitive && packet.Tag == ber.TagOctetString
}

func resultShape(op *ber.Packet) bool {
	if len(op.Children) != 3 {
		return false
	}
	for i, child := range op.Children {
		if child.ClassType != ber.ClassUniversal || child.TagType != ber.TypePrimitive {
			return false
		}
		if i == 0 {
			if child.Tag != ber.TagEnumerated || child.Data.Len() < 1 || child.Data.Len() > 2 {
				return false
			}
		} else if child.Tag != ber.TagOctetString {
			return false
		}
	}
	return true
}

func readFrame(reader io.Reader, remaining int) ([]byte, error) {
	var head [5]byte
	if remaining < 2 {
		return nil, ErrUnavailable
	}
	if _, err := io.ReadFull(reader, head[:2]); err != nil {
		return nil, err
	}
	if head[0] != 0x30 {
		return nil, ErrUnavailable
	}
	header, length := 2, int(head[1])
	if head[1]&0x80 != 0 {
		count := int(head[1] & 0x7f)
		if count == 0 || count > 3 || 2+count > remaining {
			return nil, ErrUnavailable
		}
		if _, err := io.ReadFull(reader, head[2:2+count]); err != nil {
			return nil, err
		}
		header, length = 2+count, 0
		for _, value := range head[2:header] {
			length = length*256 + int(value)
		}
	}
	total := header + length
	if total > frameLimit || total > remaining {
		return nil, ErrUnavailable
	}
	frame := make([]byte, total)
	copy(frame, head[:header])
	if _, err := io.ReadFull(reader, frame[header:]); err != nil {
		return nil, err
	}
	return frame, nil
}

func validBER(data []byte) bool {
	if len(data) == 0 || len(data) > frameLimit {
		return false
	}
	nodes := 0
	consumed, ok := berSpan(data, 1, &nodes)
	return ok && consumed == len(data)
}

func berSpan(data []byte, depth int, nodes *int) (int, bool) {
	(*nodes)++
	if depth > depthLimit || *nodes > nodeLimit || len(data) < 2 || data[0]&0x1f == 0x1f || data[0] == 0 {
		return 0, false
	}
	header, length := 2, int(data[1])
	if data[1]&0x80 != 0 {
		count := int(data[1] & 0x7f)
		if count == 0 || count > 3 || len(data) < 2+count {
			return 0, false
		}
		header, length = 2+count, 0
		for _, value := range data[2:header] {
			length = length*256 + int(value)
		}
	}
	if length > len(data)-header {
		return 0, false
	}
	end := header + length
	if data[0] == 0x02 || data[0] == 0x0a {
		if length < 1 || length > 8 {
			return 0, false
		}
	}
	if data[0]&0x20 != 0 {
		for offset := header; offset < end; {
			size, ok := berSpan(data[offset:end], depth+1, nodes)
			if !ok {
				return 0, false
			}
			offset += size
		}
	}
	return end, true
}
