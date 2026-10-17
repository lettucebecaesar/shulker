package jsonx

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) Value {
	t.Helper()
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return v
}

func TestParseBasics(t *testing.T) {
	v := mustParse(t, `{"a": 1, "b": [true, false, null], "c": "x"}`)
	o, ok := AsObject(v)
	if !ok {
		t.Fatalf("root is not an object")
	}
	if o.Len() != 3 {
		t.Fatalf("member count = %d, want 3", o.Len())
	}
	if got := o.Key(0); got != "a" {
		t.Errorf("key 0 = %q, want a", got)
	}
	if got := o.Key(1); got != "b" {
		t.Errorf("key 1 = %q, want b", got)
	}
	if n, _ := AsNumber(mustGet(t, o, "a")); n != "1" {
		t.Errorf("a = %q, want 1", n)
	}
	arr, _ := AsArray(mustGet(t, o, "b"))
	if len(arr.Items) != 3 {
		t.Fatalf("b length = %d, want 3", len(arr.Items))
	}
	if arr.Items[0] != Bool(true) || arr.Items[1] != Bool(false) {
		t.Errorf("bool literals wrong")
	}
	if _, ok := arr.Items[2].(Null); !ok {
		t.Errorf("null literal wrong")
	}
}

func mustGet(t *testing.T, o *Object, key string) Value {
	t.Helper()
	v, ok := o.Get(key)
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	return v
}

// MCBE packs tolerate BOMs, comments, and trailing commas because the
// engine's parser does. This is the tolerance bar.
func TestParseBedrockTolerances(t *testing.T) {
	src := "\xEF\xBB\xBF" + "// leading comment\n" +
		`{` + "\n" +
		`  /* block` + "\n" +
		`     comment */` + "\n" +
		`  "format_version": 2, // trailing comment` + "\n" +
		`  "arr": [1, 2, 3,],` + "\n" +
		`  "obj": {"a": 1,},` + "\n" +
		`  "b": [ ]` + "\n" +
		`}`
	v, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse with BOM/comments/trailing commas: %v", err)
	}
	o := v.(*Object)
	if o.Len() != 4 {
		t.Fatalf("members = %d, want 4", o.Len())
	}
	arr, _ := AsArray(mustGet(t, o, "arr"))
	if len(arr.Items) != 3 {
		t.Errorf("arr items = %d, want 3 (trailing comma dropped)", len(arr.Items))
	}
	obj, _ := AsObject(mustGet(t, o, "obj"))
	if obj.Len() != 1 {
		t.Errorf("obj members = %d, want 1", obj.Len())
	}
}

func TestParseDuplicateKeysPreserved(t *testing.T) {
	v := mustParse(t, `{"a": 1, "b": 2, "a": 3}`)
	o := v.(*Object)
	if o.Len() != 3 {
		t.Fatalf("members = %d, want 3 (duplicates preserved)", o.Len())
	}
	// First-match lookup mirrors the Bedrock parser.
	if n, _ := AsNumber(mustGet(t, o, "a")); n != "1" {
		t.Errorf("Get(a) = %q, want 1 (first occurrence)", n)
	}
}

func TestParseStringEscapes(t *testing.T) {
	v := mustParse(t, `{"s": "a\"b\\c\/d\b\f\n\r\teAé😀"}`)
	o := v.(*Object)
	s, _ := AsString(mustGet(t, o, "s"))
	want := "a\"b\\c/d\b\f\n\r\teAé😀"
	if s != want {
		t.Errorf("decoded = %q, want %q", s, want)
	}
	// Raw token preserved verbatim for byte-fidelity re-encode.
	m := o.Members[0]
	if m.Val.(*String).Raw != `"a\"b\\c\/d\b\f\n\r\teAé😀"` {
		t.Errorf("raw token not preserved: %s", m.Val.(*String).Raw)
	}
}

func TestParseLoneSurrogateIsReplacement(t *testing.T) {
	v := mustParse(t, `{"s": "\uD800"}`)
	o := v.(*Object)
	s, _ := AsString(mustGet(t, o, "s"))
	if s != "�" {
		t.Errorf("lone surrogate decoded = %q, want U+FFFD", s)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring of the expected error
	}{
		{"trailing garbage", `{"a":1} x`, "trailing data"},
		{"unterminated string", `{"a": "abc`, "unterminated string"},
		{"unterminated comment", `{"a": 1 /* oops`, "unterminated /* comment"},
		{"lone slash", `{/}`, "unexpected '/'"},
		{"bad literal", `{"a": tru}`, "invalid literal"},
		{"bad escape", `{"a": "\q"}`, "invalid escape"},
		{"short \\u escape", `{"a": "\u12"}`, "invalid \\u escape"},
		{"control char in string", "{\"a\": \"x\ty\"}", "control character"},
		{"bad number leading zeros", `{"a": 01}`, "expected ',' or '}'"},
		{"bad number dot", `{"a": 1.}`, "invalid number"},
		{"bad number exp", `{"a": 1e+}`, "invalid number"},
		{"missing colon", `{"a" 1}`, "expected ':'"},
		{"missing key", `{1: 2}`, "expected object key"},
		{"unterminated object", `{"a": 1`, "unterminated object"},
		{"unterminated array", `{"a": [1`, "unterminated array"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src))
			if err == nil {
				t.Fatalf("expected error for %q", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseErrorHasPosition(t *testing.T) {
	src := "{\n  \"a\": 1,\n  \"b\": @\n}"
	_, err := Parse([]byte(src))
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error is %T, want *ParseError", err)
	}
	if pe.Line != 3 {
		t.Errorf("line = %d, want 3", pe.Line)
	}
	if !strings.Contains(pe.Error(), "line 3") {
		t.Errorf("error text lacks position: %q", pe.Error())
	}
}

func TestParseDepthLimit(t *testing.T) {
	src := strings.Repeat("[", 600) + strings.Repeat("]", 600)
	if _, err := Parse([]byte(src)); err == nil {
		t.Fatal("expected depth error")
	} else if !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Untouched tokens must round-trip byte-identically through parse+encode.
// That is the property minify is built on.
func TestEncodePreservesUntouchedTokens(t *testing.T) {
	src := `{
		// comment
		"weird key /~": "\u0041\/x\t",
		"num": 0.300000011920929,
		"dup": 1, "dup": 2
	}`
	v := mustParse(t, src)
	out := string(Encode(v))
	want := `{"weird key /~":"\u0041\/x\t","num":0.300000011920929,"dup":1,"dup":2}`
	if out != want {
		t.Errorf("encode = %s\n want %s", out, want)
	}
}

func TestEncodeRoundTripStable(t *testing.T) {
	src := `{"a":[1,{"b":"c"}],"d":{"e":null,"f":true}}`
	v1 := mustParse(t, src)
	e1 := Encode(v1)
	v2 := mustParse(t, string(e1))
	e2 := Encode(v2)
	if string(e1) != string(e2) {
		t.Errorf("not stable:\n%s\n%s", e1, e2)
	}
	if string(e1) != src {
		t.Errorf("compact round trip mismatch: %s", e1)
	}
}

func TestStringSetRequotes(t *testing.T) {
	v := mustParse(t, `{"t": "old \"value\""}`)
	o := v.(*Object)
	s := mustGet(t, o, "t").(*String)
	s.Set(`new "value"`)
	out := string(Encode(v))
	want := `{"t":"new \"value\""}`
	if out != want {
		t.Errorf("encode after Set = %s, want %s", out, want)
	}
}
