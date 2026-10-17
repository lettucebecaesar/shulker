package jsonx

import (
	"fmt"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// maxDepth bounds parser recursion so a hostile or accidentally pathological
// pack cannot exhaust the stack. Real packs nest a handful of levels deep.
const maxDepth = 512

// ParseError describes a syntax problem with 1-based line/column context so
// callers can report "file + position" as the hard invariants demand.
type ParseError struct {
	Offset int
	Line   int
	Col    int
	Msg    string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Col, e.Msg)
}

type parser struct {
	data []byte
	pos  int
	line int
	col  int
}

// Parse decodes data into an order-preserving value tree.
//
// Tolerances, all of which the Bedrock engine itself accepts and which this
// parser therefore MUST accept to avoid "fixing" packs into corruption:
//   - a leading UTF-8 BOM,
//   - `//` line comments and `/* */` block comments,
//   - trailing commas in objects and arrays.
//
// Tolerances it does NOT grant (the engine wouldn't either, and silent
// guessing corrupts packs): unescaped control characters in strings, invalid
// escape sequences, invalid numbers, trailing garbage after the top-level
// value.
func Parse(data []byte) (Value, error) {
	// MCBE quirk: vanilla and community packs routinely begin with a UTF-8
	// BOM (EF BB BF) — Windows tooling loves to add them. Strip it here and
	// never write it back; stripping BOMs is a legit squash win and the
	// engine does not need them.
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	p := &parser{data: data, line: 1, col: 1}
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	v, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if p.pos != len(p.data) {
		return nil, p.errorf("unexpected trailing data after top-level value")
	}
	return v, nil
}

func (p *parser) errorf(format string, args ...any) *ParseError {
	return &ParseError{
		Offset: p.pos,
		Line:   p.line,
		Col:    p.col,
		Msg:    fmt.Sprintf(format, args...),
	}
}

func (p *parser) advance() byte {
	b := p.data[p.pos]
	p.pos++
	if b == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	return b
}

// skipWS consumes whitespace and JSONC comments. Comments and trailing
// commas are legal in Bedrock packs (the engine parses with comment flags
// enabled), so "minify" legitimately removes them — that is free squash.
func (p *parser) skipWS() error {
	for p.pos < len(p.data) {
		b := p.data[p.pos]
		switch {
		case b == ' ' || b == '\t' || b == '\r' || b == '\n':
			p.advance()
		case b == '/':
			if p.pos+1 >= len(p.data) {
				return p.errorf("unexpected end of input where a comment was starting")
			}
			switch p.data[p.pos+1] {
			case '/':
				p.advance()
				p.advance()
				for p.pos < len(p.data) && p.data[p.pos] != '\n' {
					p.advance()
				}
			case '*':
				p.advance()
				p.advance()
				closed := false
				for p.pos < len(p.data) {
					if p.data[p.pos] == '*' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/' {
						p.advance()
						p.advance()
						closed = true
						break
					}
					p.advance()
				}
				if !closed {
					return p.errorf("unterminated /* comment")
				}
			default:
				return p.errorf("unexpected '/' (expected a comment)")
			}
		default:
			return nil
		}
	}
	return nil
}

func (p *parser) parseValue(depth int) (Value, error) {
	if depth > maxDepth {
		return nil, p.errorf("nesting deeper than %d levels", maxDepth)
	}
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of input, expected a value")
	}
	switch b := p.data[p.pos]; {
	case b == '{':
		return p.parseObject(depth)
	case b == '[':
		return p.parseArray(depth)
	case b == '"':
		return p.parseString()
	case b == '-' || (b >= '0' && b <= '9'):
		return p.parseNumber()
	case b == 't':
		return p.parseLiteral("true", Bool(true))
	case b == 'f':
		return p.parseLiteral("false", Bool(false))
	case b == 'n':
		return p.parseLiteral("null", Null{})
	default:
		return nil, p.errorf("unexpected character %q", b)
	}
}

func (p *parser) parseLiteral(lit string, v Value) (Value, error) {
	if p.pos+len(lit) <= len(p.data) && string(p.data[p.pos:p.pos+len(lit)]) == lit {
		for range lit {
			p.advance()
		}
		return v, nil
	}
	return nil, p.errorf("invalid literal (expected %q)", lit)
}

func (p *parser) parseObject(depth int) (Value, error) {
	openLine, openCol := p.line, p.col
	p.advance() // '{'
	obj := &Object{}
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.advance()
		return obj, nil
	}
	for {
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, p.errorf("expected object key (object opened at line %d, column %d)", openLine, openCol)
		}
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errorf("expected ':' after object key %q", key.Str)
		}
		p.advance()
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		obj.Members = append(obj.Members, Member{Key: key.Str, KeyRaw: key.Raw, Val: val})
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated object (opened at line %d, column %d)", openLine, openCol)
		}
		switch p.data[p.pos] {
		case ',':
			// Tolerate a trailing comma before the closing brace, exactly
			// like the Bedrock parser does.
			p.advance()
			if err := p.skipWS(); err != nil {
				return nil, err
			}
			if p.pos < len(p.data) && p.data[p.pos] == '}' {
				p.advance()
				return obj, nil
			}
		case '}':
			p.advance()
			return obj, nil
		default:
			return nil, p.errorf("expected ',' or '}' in object (opened at line %d, column %d)", openLine, openCol)
		}
	}
}

func (p *parser) parseArray(depth int) (Value, error) {
	openLine, openCol := p.line, p.col
	p.advance() // '['
	arr := &Array{}
	if err := p.skipWS(); err != nil {
		return nil, err
	}
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.advance()
		return arr, nil
	}
	for {
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		arr.Items = append(arr.Items, val)
		if err := p.skipWS(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated array (opened at line %d, column %d)", openLine, openCol)
		}
		switch p.data[p.pos] {
		case ',':
			p.advance()
			if err := p.skipWS(); err != nil {
				return nil, err
			}
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.advance()
				return arr, nil
			}
		case ']':
			p.advance()
			return arr, nil
		default:
			return nil, p.errorf("expected ',' or ']' in array (opened at line %d, column %d)", openLine, openCol)
		}
	}
}

func (p *parser) parseString() (*String, error) {
	start := p.pos
	startLine, startCol := p.line, p.col
	p.advance() // opening quote
	buf := make([]byte, 0, 64)
	for {
		if p.pos >= len(p.data) {
			return nil, p.errorf("unterminated string (started at line %d, column %d)", startLine, startCol)
		}
		b := p.data[p.pos]
		switch {
		case b == '"':
			p.advance()
			return &String{Raw: string(p.data[start:p.pos]), Str: string(buf)}, nil
		case b == '\\':
			if p.pos+1 >= len(p.data) {
				return nil, p.errorf("unterminated escape sequence")
			}
			esc := p.data[p.pos+1]
			switch esc {
			case '"', '\\', '/':
				buf = append(buf, esc)
				p.advance()
				p.advance()
			case 'b':
				buf = append(buf, '\b')
				p.advance()
				p.advance()
			case 'f':
				buf = append(buf, '\f')
				p.advance()
				p.advance()
			case 'n':
				buf = append(buf, '\n')
				p.advance()
				p.advance()
			case 'r':
				buf = append(buf, '\r')
				p.advance()
				p.advance()
			case 't':
				buf = append(buf, '\t')
				p.advance()
				p.advance()
			case 'u':
				r1, ok := p.readHex4()
				if !ok {
					return nil, p.errorf("invalid \\u escape (expected four hex digits)")
				}
				// Surrogate pairs: \uD83D\uDE0D style emoji show up in
				// community pack text. Unpaired surrogates decode to
				// U+FFFD in our Str (the raw token is preserved verbatim,
				// which is what matters for byte-fidelity).
				if utf16.IsSurrogate(r1) {
					if p.pos+2 < len(p.data) && p.data[p.pos] == '\\' && p.data[p.pos+1] == 'u' {
						save := p.pos
						p.advance()
						p.advance()
						r2, ok := p.readHex4()
						if ok {
							if combined := utf16.DecodeRune(r1, r2); combined != utf8.RuneError {
								buf = utf8.AppendRune(buf, combined)
								continue
							}
						}
						p.pos = save
					}
					buf = utf8.AppendRune(buf, utf8.RuneError)
				} else {
					buf = utf8.AppendRune(buf, r1)
				}
			default:
				return nil, p.errorf("invalid escape character %q", esc)
			}
		case b < 0x20:
			return nil, p.errorf("unescaped control character 0x%02X in string", b)
		default:
			// Multi-byte UTF-8 passes through byte-wise, which is exact.
			buf = append(buf, b)
			p.advance()
		}
	}
}

// readHex4 reads the four hex digits following a `\u` whose 'u' is at
// p.data[p.pos]. It advances past all six bytes on success.
func (p *parser) readHex4() (rune, bool) {
	if p.pos+5 >= len(p.data) {
		return 0, false
	}
	v, err := strconv.ParseUint(string(p.data[p.pos+2:p.pos+6]), 16, 32)
	if err != nil {
		return 0, false
	}
	for range 6 {
		p.advance()
	}
	return rune(v), true
}

func (p *parser) parseNumber() (Value, error) {
	start := p.pos
	if p.data[p.pos] == '-' {
		p.advance()
	}
	if p.pos >= len(p.data) {
		return nil, p.errorf("unterminated number")
	}
	switch b := p.data[p.pos]; {
	case b == '0':
		p.advance()
	case b >= '1' && b <= '9':
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.advance()
		}
	default:
		return nil, p.errorf("invalid number (missing integer part)")
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.advance()
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, p.errorf("invalid number (missing digits after decimal point)")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.advance()
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.advance()
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.advance()
		}
		if p.pos >= len(p.data) || !isDigit(p.data[p.pos]) {
			return nil, p.errorf("invalid number (missing exponent digits)")
		}
		for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
			p.advance()
		}
	}
	return &Number{Raw: string(p.data[start:p.pos])}, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
