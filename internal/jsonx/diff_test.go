package jsonx

import (
	"reflect"
	"testing"
)

func TestNormalizeNumber(t *testing.T) {
	cases := []struct{ in, want string }{
		// The canonical MCBE float-noise case from vanilla
		// sound_definitions.json: 32-bit floats serialized as doubles.
		{"0.300000011920929", "0.3"},
		{"0.07000000000000001", "0.07"},
		{"0.550", "0.55"},
		{"1.0", "1"},
		{"-0.0", "-0"},
		{"2.50", "2.5"},
		{"0.80", "0.8"},
		// Integers are never rewritten, even float-shaped ones.
		{"16", "16"},
		{"0", "0"},
		{"-1", "-1"},
		{"123456789", "123456789"},
		// f32 cannot represent this exactly; the "shortest" form is not
		// shorter anyway. Untouched.
		{"123456789.0", "123456789.0"},
		// Already short.
		{"0.5", "0.5"},
		// Equal length is not a win; leave the author's spelling alone.
		{"1e2", "1e2"},
		{"1.5e2", "150"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := NormalizeNumber(tc.in); got != tc.want {
				t.Errorf("NormalizeNumber(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeNumberF32Equivalence(t *testing.T) {
	// Every normalization must be f32-bit-identical: the actual invariant.
	in := "0.300000011920929"
	out := NormalizeNumber(in)
	fa, _ := NumFloat32(in)
	fb, _ := NumFloat32(out)
	if fa != fb {
		t.Errorf("f32(%q)=%v != f32(%q)=%v", in, fa, out, fb)
	}
}

func TestNumberEqual(t *testing.T) {
	if !NumberEqual("0.550", "0.55") {
		t.Error("0.550 and 0.55 must be equal as f32")
	}
	if !NumberEqual("16", "16.0") {
		t.Error("16 and 16.0 must be equal as f32")
	}
	if NumberEqual("1", "2") {
		t.Error("1 and 2 are not equal")
	}
	if NumberEqual("abc", "xyz") {
		t.Error("unparsable literals are not equal as numbers")
	}
}

func TestWalkPointers(t *testing.T) {
	v := mustParse(t, `{"a": {"b/c": 1, "d~e": [{"f": "g"}]}}`)
	var got []string
	err := Walk(v, func(ptr string, n Value) error {
		if s, ok := n.(*String); ok {
			got = append(got, ptr+"="+s.Str)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/a/d~0e/0/f=g"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("walk pointers = %v, want %v", got, want)
	}
}

func TestGetByPointer(t *testing.T) {
	v := mustParse(t, `{"a": {"b/c": 1, "d~e": [{"f": "g"}]}}`)
	if val, ok := GetByPointer(v, "/a/d~0e/0/f"); !ok || val.(*String).Str != "g" {
		t.Errorf("GetByPointer failed: %v %v", val, ok)
	}
	if _, ok := GetByPointer(v, "/a/b~1c"); !ok {
		t.Error("expected /a/b~1c to resolve")
	}
	if _, ok := GetByPointer(v, "/a/zzz"); ok {
		t.Error("expected miss")
	}
	if _, ok := GetByPointer(v, "/a/999"); ok {
		t.Error("expected array index miss")
	}
	if val, ok := GetByPointer(v, ""); !ok || val != v {
		t.Error("empty pointer must return the root")
	}
}

func TestDiff(t *testing.T) {
	in := mustParse(t, `{"a": 1, "b": "x", "c": [1.0, 2], "d": null}`)
	out := mustParse(t, `{"a": 1, "b": "y", "c": [1, 2], "d": null}`)

	// No allowances: string change + number change reported.
	diffs := Diff(in, out, DiffOptions{})
	if len(diffs) != 2 {
		t.Fatalf("diffs = %+v, want 2 entries", diffs)
	}

	// With allowances: number normalization allowed, string rewrite allowed
	// exactly at /b from x to y.
	diffs = Diff(in, out, DiffOptions{
		AllowNumberNormalization: true,
		AllowStringChanges: map[string]StringChange{
			"/b": {From: "x", To: "y"},
		},
	})
	if len(diffs) != 0 {
		t.Fatalf("diffs with allowances = %+v, want none", diffs)
	}

	// A wrong From value is still a defect (catches rewrites applied to the
	// wrong node).
	diffs = Diff(in, out, DiffOptions{
		AllowNumberNormalization: true,
		AllowStringChanges: map[string]StringChange{
			"/b": {From: "wrong", To: "y"},
		},
	})
	if len(diffs) != 1 || diffs[0].Pointer != "/b" {
		t.Fatalf("wrong From must still report: %+v", diffs)
	}
}

func TestDiffCatchesKeyReorder(t *testing.T) {
	in := mustParse(t, `{"a": 1, "b": 2}`)
	out := mustParse(t, `{"b": 2, "a": 1}`)
	diffs := Diff(in, out, DiffOptions{})
	if len(diffs) != 1 {
		t.Fatalf("key reorder must be reported, got %+v", diffs)
	}
	if diffs[0].Pointer != "" {
		t.Errorf("pointer = %q, want root", diffs[0].Pointer)
	}
}

func TestDiffCatchesShapeChanges(t *testing.T) {
	cases := []struct {
		name    string
		in, out string
	}{
		{"type change", `{"a": 1}`, `{"a": "1"}`},
		{"member added", `{"a": 1}`, `{"a": 1, "b": 2}`},
		{"member removed", `{"a": 1, "b": 2}`, `{"a": 1}`},
		{"array length", `[1, 2]`, `[1]`},
		{"bool change", `{"a": true}`, `{"a": false}`},
		{"null removed", `{"a": null}`, `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diffs := Diff(mustParse(t, tc.in), mustParse(t, tc.out), DiffOptions{})
			if len(diffs) == 0 {
				t.Errorf("expected diff for %s -> %s", tc.in, tc.out)
			}
		})
	}
}
