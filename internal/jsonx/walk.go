package jsonx

import "strings"

// EscapePointerToken escapes a key for use in a JSON pointer token:
// '~' becomes '~0' and '/' becomes '~1' (RFC 6901).
func EscapePointerToken(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '~':
			sb.WriteString("~0")
		case '/':
			sb.WriteString("~1")
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// Walk visits every node in depth-first, document order. The root is
// delivered with pointer "" (an empty pointer, like encoding/json's
// convention for "the whole document"). Object keys and array indexes form
// the path segments, so error reports and rewrite logs can say exactly where
// in a file something lives: file + JSON pointer is the invariant-required
// error context.
func Walk(v Value, fn func(pointer string, v Value) error) error {
	return walk("", v, fn)
}

func walk(ptr string, v Value, fn func(string, Value) error) error {
	if err := fn(ptr, v); err != nil {
		return err
	}
	switch t := v.(type) {
	case *Object:
		for i := range t.Members {
			m := &t.Members[i]
			child := ptr + "/" + EscapePointerToken(m.Key)
			if err := walk(child, m.Val, fn); err != nil {
				return err
			}
		}
	case *Array:
		for i, item := range t.Items {
			child := ptr + "/" + itoa(i)
			if err := walk(child, item, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func itoa(i int) string {
	if i >= 0 && i < 10 {
		return string(rune('0' + i))
	}
	// strconv for the general case; the fast path above keeps the common
	// case allocation-free.
	var buf [8]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

// GetByPointer resolves a JSON pointer ("" = root, segments per RFC 6901
// with ~0/~1 unescaping) produced by Walk, Graph edges, or mapping files.
// Resolution follows first-match semantics for duplicate keys, mirroring the
// engine's parser.
func GetByPointer(root Value, pointer string) (Value, bool) {
	if pointer == "" {
		return root, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	cur := root
	rest := pointer[1:]
	for {
		var token string
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			token, rest = rest[:i], rest[i+1:]
		} else {
			token, rest = rest, ""
		}
		token = unescapePointerToken(token)
		switch o := cur.(type) {
		case *Object:
			v, ok := o.Get(token)
			if !ok {
				return nil, false
			}
			cur = v
		case *Array:
			idx := 0
			valid := token != "" && token[0] >= '0' && token[0] <= '9'
			if valid {
				for j := 0; j < len(token); j++ {
					if token[j] < '0' || token[j] > '9' {
						valid = false
						break
					}
					idx = idx*10 + int(token[j]-'0')
				}
			}
			if !valid || idx >= len(o.Items) {
				return nil, false
			}
			cur = o.Items[idx]
		default:
			return nil, false
		}
		if rest == "" {
			return cur, true
		}
	}
}

func unescapePointerToken(s string) string {
	if !strings.Contains(s, "~") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '~' && i+1 < len(s) {
			switch s[i+1] {
			case '0':
				sb.WriteByte('~')
				i++
				continue
			case '1':
				sb.WriteByte('/')
				i++
				continue
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}
