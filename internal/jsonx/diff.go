package jsonx

// StringChange is one expected string rewrite: at a given JSON pointer the
// input value From should have become To. Anything else is a defect.
type StringChange struct {
	From string
	To   string
}

// DiffOptions controls value comparison:
//
//   - AllowStringChanges permits specific string rewrites (the transform log
//     supplies them); nil forbids all.
//   - AllowNumberNormalization treats two literals as equal when they parse
//     to the same float32 (minify's provably-safe float shortening).
type DiffOptions struct {
	AllowStringChanges       map[string]StringChange
	AllowNumberNormalization bool
}

// Difference is one place where output diverges from input beyond what the
// options explicitly allow. Pointer locates the node; Reason explains it.
type Difference struct {
	Pointer string
	Reason  string
}

// Diff compares an input tree against an output tree and reports every
// divergence. This is the post-transform validator's core: a squash may only
// change a document at pointers it logged in advance, and even then only from
// the recorded From to the recorded To. Key order is compared strictly —
// "preserve JSON key order" is a hard invariant.
func Diff(in, out Value, opts DiffOptions) []Difference {
	var diffs []Difference
	diff("", in, out, opts, &diffs)
	return diffs
}

func diff(ptr string, in, out Value, opts DiffOptions, acc *[]Difference) {
	switch iv := in.(type) {
	case *Object:
		ov, ok := out.(*Object)
		if !ok {
			*acc = append(*acc, Difference{ptr, "object became " + kind(out)})
			return
		}
		if len(iv.Members) != len(ov.Members) {
			*acc = append(*acc, Difference{ptr, "member count changed"})
			return
		}
		for i := range iv.Members {
			im := &iv.Members[i]
			om := &ov.Members[i]
			if im.Key != om.Key {
				*acc = append(*acc, Difference{ptr, "member order or key changed: " +
					im.Key + " -> " + om.Key})
				return
			}
			diff(ptr+"/"+EscapePointerToken(im.Key), im.Val, om.Val, opts, acc)
		}
	case *Array:
		ov, ok := out.(*Array)
		if !ok {
			*acc = append(*acc, Difference{ptr, "array became " + kind(out)})
			return
		}
		if len(iv.Items) != len(ov.Items) {
			*acc = append(*acc, Difference{ptr, "array length changed"})
			return
		}
		for i := range iv.Items {
			diff(ptr+"/"+itoa(i), iv.Items[i], ov.Items[i], opts, acc)
		}
	case *String:
		switch ov := out.(type) {
		case *String:
			if iv.Str == ov.Str {
				return
			}
			if allowed, ok := opts.AllowStringChanges[ptr]; ok &&
				allowed.From == iv.Str && allowed.To == ov.Str {
				return
			}
			*acc = append(*acc, Difference{ptr, "string changed: " +
				iv.Str + " -> " + ov.Str})
		default:
			*acc = append(*acc, Difference{ptr, "string became " + kind(out)})
		}
	case *Number:
		switch out.(type) {
		case *Number:
			oin := RawNumber(out)
			if iv.Raw == oin {
				return
			}
			if opts.AllowNumberNormalization && NumberEqual(iv.Raw, oin) {
				return
			}
			*acc = append(*acc, Difference{ptr, "number changed: " + iv.Raw + " -> " + oin})
		default:
			*acc = append(*acc, Difference{ptr, "number became " + kind(out)})
		}
	case Bool:
		ob, ok := out.(Bool)
		if !ok || ob != iv {
			*acc = append(*acc, Difference{ptr, "bool changed"})
		}
	case Null:
		if _, ok := out.(Null); !ok {
			*acc = append(*acc, Difference{ptr, "null became " + kind(out)})
		}
	}
}

func kind(v Value) string {
	switch v.(type) {
	case *Object:
		return "object"
	case *Array:
		return "array"
	case *String:
		return "string"
	case *Number:
		return "number"
	case Bool:
		return "bool"
	case Null:
		return "null"
	default:
		return "unknown"
	}
}
