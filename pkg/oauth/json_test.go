package oauth

import (
	"fmt"
	"strings"
	"testing"
)

func TestSubjectExactTypedIdentityAndUnicode(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       Identity
	}{
		{"integer", `{"account":{"id":1}}`, Identity{Kind: SubjectInteger, Subject: "1"}},
		{"string", `{"account":{"id":"1"}}`, Identity{Kind: SubjectString, Subject: "1"}},
		{"zero", `{"account":{"id":0}}`, Identity{Kind: SubjectInteger, Subject: "0"}},
		{"large", `{"account":{"id":9007199254740993123456789}}`, Identity{Kind: SubjectInteger, Subject: "9007199254740993123456789"}},
		{"exact_spaces_case", `{"account":{"id":" A a "}}`, Identity{Kind: SubjectString, Subject: " A a "}},
		{"unicode_pair", `{"account":{"id":"\ud83d\ude00"}}`, Identity{Kind: SubjectString, Subject: "😀"}},
		{"replacement_character", `{"account":{"id":"�"}}`, Identity{Kind: SubjectString, Subject: "�"}},
		{"escaped_quote", `{"account":{"id":"a\"b\\c"}}`, Identity{Kind: SubjectString, Subject: "a\"b\\c"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := subject([]byte(test.body), []string{"account", "id"})
			if err != nil || got != test.want {
				t.Fatalf("exact typed subject: got=%v err=%v", got, err)
			}
		})
	}
}

func TestSubjectRejectsAmbiguousMissingAndMalformedJSON(t *testing.T) {
	cases := map[string]string{
		"decimal": `{"account":{"id":1.0}}`, "exponent": `{"account":{"id":1e0}}`,
		"negative": `{"account":{"id":-1}}`, "negative_zero": `{"account":{"id":-0}}`,
		"leading_zero": `{"account":{"id":01}}`, "null": `{"account":{"id":null}}`,
		"boolean": `{"account":{"id":true}}`, "array": `{"account":{"id":[1]}}`,
		"object": `{"account":{"id":{"id":"a"}}}`, "empty": `{"account":{"id":""}}`,
		"missing": `{"account":{"email":"not identity"}}`, "wrong_parent": `{"account":[{"id":"a"}]}`,
		"duplicate_subject": `{"account":{"id":"a","id":"b"}}`,
		"escaped_duplicate": `{"account":{"id":"a","\u0069d":"a"}}`,
		"unused_duplicate":  `{"account":{"id":"a"},"profile":{"email":"a","email":"b"}}`,
		"surrogate_high":    `{"account":{"id":"\ud800"}}`, "surrogate_low": `{"account":{"id":"\udc00"}}`,
		"surrogate_wrong_pair": `{"account":{"id":"\ud800\u0041"}}`,
		"surrogate_key":        `{"account":{"id":"a","\ud800":"ignored"}}`,
		"nul":                  `{"account":{"id":"a\u0000b"}}`, "root_array": `[{"account":{"id":"a"}}]`,
		"trailing_document": `{"account":{"id":"a"}} {}`,
		"invalid_utf8":      "{\"account\":{\"id\":\"" + string([]byte{0xff}) + "\"}}",
		"long_string_id":    `{"account":{"id":"` + strings.Repeat("a", maxSubjectBytes+1) + `"}}`,
		"long_integer_id":   `{"account":{"id":` + strings.Repeat("1", maxSubjectBytes+1) + `}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := subject([]byte(body), []string{"account", "id"})
			if err != ErrProtocol || got != (Identity{}) {
				t.Fatalf("malformed subject accepted: %v %v", got, err)
			}
		})
	}
}

func TestStrictJSONFiniteDepthCardinalityAndStrings(t *testing.T) {
	members := make([]string, maxObjectMembers+1)
	for i := range members {
		members[i] = fmt.Sprintf("\"k%d\":0", i)
	}
	array := strings.TrimSuffix(strings.Repeat("0,", maxArrayElements+1), ",")
	oneArray := "[" + strings.TrimSuffix(strings.Repeat("0,", maxArrayElements), ",") + "]"
	nodeOverflow := "[" + strings.TrimSuffix(strings.Repeat(oneArray+",", 9), ",") + "]"
	cases := map[string]string{
		"depth":          `{"id":"a","unused":` + strings.Repeat("[", maxJSONDepth+1) + "0" + strings.Repeat("]", maxJSONDepth+1) + "}",
		"members":        "{" + strings.Join(members, ",") + "}",
		"array":          `{"id":"a","unused":[` + array + "]}",
		"nodes":          `{"id":"a","unused":` + nodeOverflow + "}",
		"decoded_string": `{"id":"a","unused":"` + strings.Repeat("x", maxJSONStringBytes+1) + "\"}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := subject([]byte(body), []string{"id"}); err != ErrProtocol {
				t.Fatalf("unbounded JSON admitted: %v", err)
			}
		})
	}
}

func TestBearerGrammarRejectsHeaderInjectionAndCoercion(t *testing.T) {
	for _, value := range []string{"", "=", "a=b", "a b", "a\r\nX: secret", "é", strings.Repeat("a", 4097)} {
		if bearer(value) {
			t.Fatal("invalid bearer admitted")
		}
	}
	for _, value := range []string{"a", "a+/._~-", "a===", strings.Repeat("a", 4096)} {
		if !bearer(value) {
			t.Fatal("valid bearer rejected")
		}
	}
}
