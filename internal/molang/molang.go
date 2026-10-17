// Package molang is shulker's Molang tokenizer seam.
//
// Molang is the expression language embedded in JSON string values all over
// Bedrock resource packs: animations, render controllers, particle
// components, client entity scripts, json-ui bindings. It can reference
// variable.*, query.*, texture/geometry/material map keys from the client
// entity, and single-quoted string literals.
//
// v0.1 POLICY (hard invariant 5): Molang string literals are sacred. This
// package exists so the rest of shulker can identify expression strings and
// refuse to touch them. It is a seam, deliberately not a rewriter — the
// v0.2+ roadmap (docs/ROADMAP.md) is where actual Molang tooling plugs in.
package molang

import "strings"

// Strong expression markers. Plain pack data (texture paths, identifiers,
// short names) never contains these, which is what makes the heuristic safe:
// false positives merely make shulker skip a transform it was going to skip
// anyway (conservatism beats compression); false negatives would let a
// Molang string be treated as data, which the whole-string file-resolution
// rules make practically impossible.
var expressionMarkers = []string{
	"query.", "variable.", "math.", "template.", "target.",
	"(", "==", "!=", "&&", "||", "?", "return ", " this", ";",
}

// IsExpression reports whether a JSON string value looks like a Molang
// expression rather than plain data. Animations, render controllers, and
// scripts are full of these; texture paths and identifiers are not.
func IsExpression(s string) bool {
	for _, m := range expressionMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// Kind classifies a Molang token.
type Kind uint8

const (
	Ident   Kind = iota // identifiers, keywords, dotted paths: query.anim_time
	Number              // 1, 1.5, 0.25
	Str                 // 'single quoted string literal'
	Op                  // + - * / % = ! < > & | ? : etc.
	Delim               // ( ) [ ] { } , ;
	Space               // whitespace (kept so re-emission is lossless)
	Invalid             // anything the tokenizer cannot classify
)

func (k Kind) String() string {
	switch k {
	case Ident:
		return "ident"
	case Number:
		return "number"
	case Str:
		return "string"
	case Op:
		return "op"
	case Delim:
		return "delim"
	case Space:
		return "space"
	default:
		return "invalid"
	}
}

// Token is one lexical element of a Molang expression.
type Token struct {
	Kind Kind
	Text string
}

// Tokenize lexics a Molang expression into tokens. It never fails loudly on
// odd input — unknown bytes become Invalid tokens — because v0.1 only uses
// this to recognize and protect expression strings, never to rewrite them.
// When v0.2 grows a real Molang minifier/renamer, this becomes its front end
// and can be tightened.
func Tokenize(s string) []Token {
	var out []Token
	i := 0
	push := func(k Kind, text string) { out = append(out, Token{Kind: k, Text: text}) }
	for i < len(s) {
		b := s[i]
		switch {
		case b == ' ' || b == '\t' || b == '\n' || b == '\r':
			j := i
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			push(Space, s[i:j])
			i = j
		case b == '\'':
			// Molang string literals are single-quoted. The sacred thing.
			j := i + 1
			for j < len(s) && s[j] != '\'' {
				j++
			}
			if j < len(s) {
				j++
			}
			push(Str, s[i:j])
			i = j
		case b >= '0' && b <= '9' || (b == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'):
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			push(Number, s[i:j])
			i = j
		case isIdentStart(b):
			j := i
			for j < len(s) {
				if isIdentStart(s[j]) || s[j] >= '0' && s[j] <= '9' {
					j++
					continue
				}
				// Dotted identifier paths (math.sin, query.anim_time,
				// variable.particle_age) belong to the identifier token.
				if s[j] == '.' && j+1 < len(s) && isIdentStart(s[j+1]) {
					j++
					continue
				}
				break
			}
			push(Ident, s[i:j])
			i = j
		case strings.IndexByte("+-*/%=!<>&|?:", b) >= 0:
			push(Op, s[i:i+1])
			i++
		case strings.IndexByte("()[]{},;", b) >= 0:
			push(Delim, s[i:i+1])
			i++
		default:
			push(Invalid, s[i:i+1])
			i++
		}
	}
	return out
}

func isIdentStart(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}
