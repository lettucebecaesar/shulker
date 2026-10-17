// Package jsonx is shulker's order-preserving, tolerant JSON engine.
//
// Why not encoding/json? Three MCBE-specific reasons:
//
//  1. The Bedrock engine reads JSON with a RapidJSON-based parser that has
//     comment support and trailing-comma support enabled. Vanilla itself ships
//     files containing `//` comments, and community packs routinely ship
//     comments, trailing commas, and UTF-8 BOMs. stdlib encoding/json fails
//     on all three, so it cannot even load the packs we must not corrupt.
//  2. MCBE JSON is semantically order-sensitive in places registries rely on
//     (and pack authors eyeball diffs of these files). We preserve member
//     order — including duplicate keys — byte-for-byte on re-encode.
//  3. The rewriter must mutate individual string values (path rewrites) while
//     leaving every other token exactly as it was written, so that
//     "the pack loads the same" is provable rather than hopeful.
//
// Numbers are stored as raw literals: shulker never re-formats a number unless
// normalization is provably value-identical (see NormalizeNumber).
package jsonx

// Value is any JSON value node. All implementations are pointer-free except
// Object and Array which are used by pointer so the rewriter can hold stable
// references into the tree.
type Value interface {
	isValue()
}

// Object preserves member order and duplicate keys. The Bedrock engine's
// parser keeps the first occurrence of a duplicate key; Get matches that.
type Object struct {
	Members []Member
}

// Member is one key/value pair of an object. KeyRaw is the original quoted
// token (including quotes) so untouched members re-encode byte-identically;
// it is empty when the member was constructed programmatically.
type Member struct {
	Key    string
	KeyRaw string
	Val    Value
}

// Array is a JSON array.
type Array struct {
	Items []Value
}

// String is a JSON string. Raw includes the surrounding quotes and preserves
// the original escapes; Str is the decoded value. If Raw is empty the string
// was constructed programmatically and encoding re-escapes Str.
type String struct {
	Raw string
	Str string
}

// Number stores the raw literal. JSON numbers are never reformatted except
// through NormalizeNumber's provably-safe shortening.
type Number struct {
	Raw string
}

// Bool is a JSON boolean.
type Bool bool

// Null is JSON null. Vanilla's sound_definitions.json uses null for
// min_distance/max_distance, so this is a first-class value here.
type Null struct{}

func (*Object) isValue() {}
func (*Array) isValue()  {}
func (*String) isValue()  {}
func (*Number) isValue()  {}
func (Bool) isValue()     {}
func (Null) isValue()     {}

// Get returns the value of the first member with the given decoded key.
func (o *Object) Get(key string) (Value, bool) {
	for i := range o.Members {
		if o.Members[i].Key == key {
			return o.Members[i].Val, true
		}
	}
	return nil, false
}

// Key returns the first member's decoded key at index i.
func (o *Object) Key(i int) string { return o.Members[i].Key }

// Len returns the number of members.
func (o *Object) Len() int { return len(o.Members) }

// String returns the decoded content of a string value.
func (s *String) String() string { return s.Str }

// Set replaces the string's content, re-encoding Raw from the new value.
// This is the mutation the path rewriter performs; everything else about
// the document stays untouched.
func (s *String) Set(v string) {
	s.Str = v
	s.Raw = Quote(v)
}

// AsObject narrows a Value to *Object.
func AsObject(v Value) (*Object, bool) {
	o, ok := v.(*Object)
	return o, ok
}

// AsArray narrows a Value to *Array.
func AsArray(v Value) (*Array, bool) {
	a, ok := v.(*Array)
	return a, ok
}

// AsString narrows a Value to *String and returns its decoded content.
func AsString(v Value) (string, bool) {
	if s, ok := v.(*String); ok {
		return s.Str, true
	}
	return "", false
}

// AsStringNode narrows a Value to *String (the mutable node itself).
func AsStringNode(v Value) (*String, bool) {
	s, ok := v.(*String)
	return s, ok
}

// AsNumber returns the raw literal of a number value.
func AsNumber(v Value) (string, bool) {
	if n, ok := v.(*Number); ok {
		return n.Raw, true
	}
	return "", false
}
