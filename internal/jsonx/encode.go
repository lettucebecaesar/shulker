package jsonx

import (
	"io"
	"strconv"
	"strings"
)

// Encode serializes v in compact form. Untouched tokens re-emit their exact
// original bytes (raw strings, raw keys, raw number literals); only values
// that were programmatically constructed or Set() get re-escaped. That is what
// makes minify provably non-destructive.
func Encode(v Value) []byte {
	var sb strings.Builder
	write(&sb, v)
	return []byte(sb.String())
}

// Write serializes v to w in compact form.
func Write(w io.Writer, v Value) error {
	var sb strings.Builder
	write(&sb, v)
	_, err := io.WriteString(w, sb.String())
	return err
}

func write(sb *strings.Builder, v Value) {
	switch t := v.(type) {
	case *Object:
		sb.WriteByte('{')
		for i := range t.Members {
			if i > 0 {
				sb.WriteByte(',')
			}
			m := &t.Members[i]
			if m.KeyRaw != "" {
				sb.WriteString(m.KeyRaw)
			} else {
				sb.WriteString(Quote(m.Key))
			}
			sb.WriteByte(':')
			write(sb, m.Val)
		}
		sb.WriteByte('}')
	case *Array:
		sb.WriteByte('[')
		for i, item := range t.Items {
			if i > 0 {
				sb.WriteByte(',')
			}
			write(sb, item)
		}
		sb.WriteByte(']')
	case *String:
		if t.Raw != "" {
			sb.WriteString(t.Raw)
		} else {
			sb.WriteString(Quote(t.Str))
		}
	case *Number:
		sb.WriteString(t.Raw)
	case Bool:
		if t {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case Null:
		sb.WriteString("null")
	}
}

// Quote returns s as a JSON string literal, including surrounding quotes.
// Escaping is the minimal deterministic set: quote, backslash, and control
// characters (with the standard single-letter escapes where they exist).
// Non-ASCII characters are emitted as raw UTF-8 — shorter than \u escapes and
// byte-identical for round-trips.
func Quote(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == '"':
			sb.WriteString(`\"`)
		case b == '\\':
			sb.WriteString(`\\`)
		case b == '\b':
			sb.WriteString(`\b`)
		case b == '\f':
			sb.WriteString(`\f`)
		case b == '\n':
			sb.WriteString(`\n`)
		case b == '\r':
			sb.WriteString(`\r`)
		case b == '\t':
			sb.WriteString(`\t`)
		case b < 0x20:
			sb.WriteString(`\u00`)
			const hexDigits = "0123456789abcdef"
			sb.WriteByte(hexDigits[b>>4])
			sb.WriteByte(hexDigits[b&0xF])
		default:
			sb.WriteByte(b)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// RawNumber returns the raw literal of a Number value ("0" for non-numbers).
func RawNumber(v Value) string {
	if n, ok := v.(*Number); ok {
		return n.Raw
	}
	return ""
}

// NumFloat parses a raw literal as float32 bits. ok is false for unparsable
// literals (callers treat that as "do not touch").
func NumFloat32(raw string) (float32, bool) {
	f, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		return 0, false
	}
	return float32(f), true
}
