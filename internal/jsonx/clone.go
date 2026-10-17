package jsonx

// Clone deep-copies a value tree. The transform pipeline works on clones so
// the post-transform validator can diff against untouched input trees —
// mutation via graph edges must never corrupt the evidence.
func Clone(v Value) Value {
	switch t := v.(type) {
	case *Object:
		out := &Object{Members: make([]Member, len(t.Members))}
		for i := range t.Members {
			out.Members[i] = Member{
				Key:    t.Members[i].Key,
				KeyRaw: t.Members[i].KeyRaw,
				Val:    Clone(t.Members[i].Val),
			}
		}
		return out
	case *Array:
		out := &Array{Items: make([]Value, len(t.Items))}
		for i, item := range t.Items {
			out.Items[i] = Clone(item)
		}
		return out
	case *String:
		c := *t
		return &c
	case *Number:
		c := *t
		return &c
	default:
		return v // Bool, Null are immutable value types
	}
}

// CloneAll clones every entry of a document map.
func CloneAll(docs map[string]Value) map[string]Value {
	out := make(map[string]Value, len(docs))
	for k, v := range docs {
		out[k] = Clone(v)
	}
	return out
}
