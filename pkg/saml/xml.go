package saml

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"unicode/utf8"
)

type element struct {
	name       xml.Name
	attributes map[xml.Name]string
	children   []*element
	value      strings.Builder
	parent     *element
}

// A bounded namespace-aware admission pass precedes etree and signature work.
// It is not XML signature validation or a replacement SAML parser. Globally
// stable named-prefix bindings prevent divergent descendant-prefix serialization.
func parseXML(ctx context.Context, raw []byte) (*element, error) {
	if len(raw) == 0 || len(raw) > MaxResponseBytes || !utf8.Valid(raw) {
		return nil, ErrProtocol
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = true
	var root *element
	stack := []*element{}
	ids := map[string]bool{}
	namespaces := map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}
	nodes, tokens := 0, 0
	declaration := false
	for {
		if ctx.Err() != nil {
			return nil, ErrUnavailable
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrProtocol
		}
		tokens++
		if tokens > 16384 {
			return nil, ErrProtocol
		}
		switch token := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 4096 || len(stack) >= 32 || len(token.Attr) > 32 {
				return nil, ErrProtocol
			}
			switch token.Name.Space {
			case assertionNS, protocolNS, signatureNS, exclusiveC14N:
			default:
				return nil, ErrProtocol
			}
			e := &element{name: token.Name, attributes: map[xml.Name]string{}}
			for _, a := range token.Attr {
				if _, exists := e.attributes[a.Name]; exists || len(a.Value) > 8192 {
					return nil, ErrProtocol
				}
				e.attributes[a.Name] = a.Value
				prefix, isNS := "", false
				if a.Name.Space == "xmlns" {
					prefix = a.Name.Local
					isNS = true
				} else if a.Name.Space == "" && a.Name.Local == "xmlns" {
					isNS = true
				}
				if isNS {
					if prior, ok := namespaces[prefix]; prefix != "" && ok && prior != a.Value {
						return nil, ErrProtocol
					}
					if a.Value == "" || prefix == "xmlns" {
						return nil, ErrProtocol
					}
					// Default namespaces are scoped by encoding/xml expanded names.
					// Only named prefixes retain the document-wide anti-rebinding guard.
					if prefix != "" {
						namespaces[prefix] = a.Value
					}
					continue
				}
				if a.Name.Space != "" {
					return nil, ErrProtocol
				}
				// All alternate ID spellings are refused, not silently ignored.
				if strings.EqualFold(a.Name.Local, "id") {
					if a.Name.Local != "ID" || !xmlID(a.Value, 1) || ids[a.Value] {
						return nil, ErrProtocol
					}
					ids[a.Value] = true
				}
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, ErrProtocol
				}
				root = e
			} else {
				e.parent = stack[len(stack)-1]
				e.parent.children = append(e.parent.children, e)
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != token.Name {
				return nil, ErrProtocol
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(token)) != "" {
					return nil, ErrProtocol
				}
				continue
			}
			e := stack[len(stack)-1]
			if e.value.Len()+len(token) > 65536 {
				return nil, ErrProtocol
			}
			_, _ = e.value.Write(token) // strings.Builder.Write always returns nil error.
		case xml.ProcInst:
			if root != nil || declaration || token.Target != "xml" || !strings.HasPrefix(string(token.Inst), `version="1.0"`) || (strings.Contains(string(token.Inst), "encoding") && !strings.Contains(string(token.Inst), `encoding="UTF-8"`)) {
				return nil, ErrProtocol
			}
			declaration = true
		default:
			return nil, ErrProtocol // No DTD/entities, comments or processing instructions.
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, ErrProtocol
	}
	return root, nil
}
func (e *element) attr(name string) string { return e.attributes[xml.Name{Local: name}] }
func (e *element) named(ns, name string) bool {
	return e != nil && e.name == (xml.Name{Space: ns, Local: name})
}
func (e *element) childrenNamed(ns, name string) []*element {
	var result []*element
	for _, child := range e.children {
		if child.named(ns, name) {
			result = append(result, child)
		}
	}
	return result
}
func (e *element) only(ns, name string) (*element, error) {
	found := e.childrenNamed(ns, name)
	if len(found) != 1 {
		return nil, ErrProtocol
	}
	return found[0], nil
}
func (e *element) attrs(allowed ...string) bool {
	for a := range e.attributes {
		if a.Space == "xmlns" || a.Space == "" && a.Local == "xmlns" {
			continue
		}
		ok := false
		for _, name := range allowed {
			if a.Space == "" && a.Local == name {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
func (e *element) simple() bool    { return len(e.children) == 0 }
func (e *element) container() bool { return strings.TrimSpace(e.value.String()) == "" }
func childrenAllowed(e *element, ns string, names ...string) bool {
	if !e.container() {
		return false
	}
	for _, child := range e.children {
		allowed := false
		for _, name := range names {
			if child.named(ns, name) {
				allowed = true
			}
		}
		for _, name := range names {
			if name == "Signature" && child.named(signatureNS, "Signature") {
				allowed = true
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}
