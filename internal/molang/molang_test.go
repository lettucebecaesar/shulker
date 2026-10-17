package molang

import "testing"

func TestIsExpression(t *testing.T) {
	exprs := []string{
		"query.modified_move_speed",
		"v.index = (t.variant == 'temperate') ? 0 : 2;",
		"math.sin(query.anim_time * 90) * 10",
		"query.is_baby ? 2.0 : 1.0",
		"variable.size = 0.75 * math.pow(1.0 - (variable.particle_age / variable.particle_lifetime), 0.08);",
		"(query.x ? 'a' : 'b')",
	}
	for _, s := range exprs {
		if !IsExpression(s) {
			t.Errorf("IsExpression(%q) = false, want true", s)
		}
	}
	plain := []string{
		"textures/custom/block",
		"textures/entity/pig/pig_v3.png",
		"geometry.pig.v3",
		"controller.render.pig",
		"shulker.custom_block", // a short name
		"entity_alphatest",
		"en_US",
		"#df6827",
		"spawn_egg_pig",
	}
	for _, s := range plain {
		if IsExpression(s) {
			t.Errorf("IsExpression(%q) = true, want false", s)
		}
	}
}

func TestTokenize(t *testing.T) {
	toks := Tokenize("math.sin(query.anim_time * 90) ? 'a' : 1.5")
	wantKinds := []Kind{Ident, Delim, Ident, Space, Op, Space, Number, Delim, Space, Op, Space, Str, Space, Op, Space, Number}
	if len(toks) != len(wantKinds) {
		t.Fatalf("token count = %d (%+v), want %d", len(toks), toks, len(wantKinds))
	}
	for i, k := range wantKinds {
		if toks[i].Kind != k {
			t.Errorf("token[%d] = %v (%q), want %v", i, toks[i].Kind, toks[i].Text, k)
		}
	}
	// The string literal must survive intact — sacred content.
	found := false
	for _, tok := range toks {
		if tok.Kind == Str && tok.Text == "'a'" {
			found = true
		}
	}
	if !found {
		t.Errorf("string literal not preserved: %+v", toks)
	}
}
