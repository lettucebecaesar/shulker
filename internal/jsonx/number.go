package jsonx

import "strconv"

// NormalizeNumber shortens a JSON number literal only when the result is
// provably value-identical, from the point of view of the Minecraft Bedrock
// engine.
//
// MCBE quirk: the engine consumes JSON numbers through 32-bit float paths
// (the same reason PackSquash normalizes as f32 on the Java side of the
// fence). Vanilla's own sound_definitions.json is littered with artifacts of
// 32-bit floats serialized as 64-bit doubles, e.g.
//
//	"volume" : 0.07000000000000001
//
// which is just 0.07 once rounded through float32. The round-trip bar the
// design demands:
//
//  1. Only literals containing '.' or an exponent are considered. Integer
//     literals are never rewritten — some engine code paths branch on
//     "is this an int", and "16" must never become "1.6e1".
//  2. Both the original and the shortened form must parse to the exact same
//     float32 bits (PackSquash-style provable equivalence).
//  3. The shortened form must actually be shorter.
//
// Examples: 0.300000011920929 → 0.3, 0.550 → 0.55, 1.0 → 1,
// 123456789.0 → stays (float32 cannot represent it exactly, and the shortest
// round-trip form "1.23456792e+08" is longer anyway).
func NormalizeNumber(raw string) string {
	if !containsFloatMarker(raw) {
		return raw
	}
	f, err := strconv.ParseFloat(raw, 32)
	if err != nil {
		// Overflow to ±Inf, underflow, or garbage: never touch.
		return raw
	}
	f32 := float32(f)
	short := strconv.FormatFloat(float64(f32), 'g', -1, 32)
	if len(short) >= len(raw) {
		return raw
	}
	f2, err := strconv.ParseFloat(short, 32)
	if err != nil {
		return raw
	}
	if float32(f2) != f32 {
		return raw
	}
	return short
}

func containsFloatMarker(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' || s[i] == 'e' || s[i] == 'E' {
			return true
		}
	}
	return false
}

// NumberEqual reports whether two raw literals denote the same float32 value.
// Used by the post-transform validator: minified output may legitimately
// differ textually (normalization) but never numerically.
func NumberEqual(a, b string) bool {
	if a == b {
		return true
	}
	fa, errA := strconv.ParseFloat(a, 32)
	fb, errB := strconv.ParseFloat(b, 32)
	if errA != nil || errB != nil {
		return false
	}
	return float32(fa) == float32(fb)
}
