package pack

import (
	"strings"
	"testing"
)

func TestParseManifestValid(t *testing.T) {
	m, findings := ParseManifest([]byte(validManifest))
	if m == nil {
		t.Fatal("manifest did not parse")
	}
	if HasErrors(findings) {
		t.Fatalf("unexpected errors: %+v", findings)
	}
	if m.Name != "Test Pack" {
		t.Errorf("name = %q", m.Name)
	}
	if m.UUID != "66c6e9a8-3093-462a-9c36-dbb052165822" {
		t.Errorf("uuid = %q", m.UUID)
	}
	if len(m.Modules) != 1 || m.Modules[0].Type != "resources" {
		t.Errorf("modules = %+v", m.Modules)
	}
	if len(m.MinEngineVersion) != 3 || m.MinEngineVersion[2] != 0 {
		t.Errorf("min_engine_version = %v", m.MinEngineVersion)
	}
}

func TestParseManifestLegacyFormatVersionWarns(t *testing.T) {
	src := strings.Replace(validManifest, `"format_version": 2`, `"format_version": 1`, 1)
	m, findings := ParseManifest([]byte(src))
	if m == nil || HasErrors(findings) {
		t.Fatalf("expected warnings only: %+v", findings)
	}
	warned := false
	for _, f := range findings {
		if f.Severity == SeverityWarning && strings.Contains(f.Msg, "format_version 1") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("missing format_version warning: %+v", findings)
	}
}

func TestParseManifestBadUUID(t *testing.T) {
	src := strings.Replace(validManifest,
		"66c6e9a8-3093-462a-9c36-dbb052165822", "not-a-uuid", 1)
	_, findings := ParseManifest([]byte(src))
	if !HasErrors(findings) {
		t.Fatal("expected uuid error")
	}
}

func TestParseManifestModuleUUIDCollision(t *testing.T) {
	src := strings.Replace(validManifest,
		"743f6949-53be-44b6-b326-398005028819", "66c6e9a8-3093-462a-9c36-dbb052165822", 1)
	_, findings := ParseManifest([]byte(src))
	found := false
	for _, f := range findings {
		if f.Severity == SeverityError && strings.Contains(f.Msg, "equals header.uuid") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected module/header uuid collision error, got %+v", findings)
	}
}

func TestParseManifestMissingModules(t *testing.T) {
	src := `{"format_version": 2, "header": {
		"uuid": "66c6e9a8-3093-462a-9c36-dbb052165822",
		"version": [1, 0, 0]}}`
	_, findings := ParseManifest([]byte(src))
	if !HasErrors(findings) {
		t.Fatal("expected missing-modules error")
	}
}

func TestParseManifestToleratesComments(t *testing.T) {
	// Community packs write comments into manifests. We must parse what the
	// engine parses; and since the manifest is stored verbatim, the comments
	// survive untouched in the output too.
	src := `{
		// TODO: change this UUID before shipping
		"format_version": 2,
		"header": {"name": "x", "uuid": "66c6e9a8-3093-462a-9c36-dbb052165822", "version": [1,0,0]},
		"modules": [{"type": "resources", "uuid": "743f6949-53be-44b6-b326-398005028819", "version": [1,0,0],},]
	}`
	m, findings := ParseManifest([]byte(src))
	if m == nil || HasErrors(findings) {
		t.Fatalf("expected success with comments/trailing commas: %+v", findings)
	}
}

func TestParseManifestCapabilities(t *testing.T) {
	src := strings.Replace(validManifest, `]
}`, `],
	"capabilities": ["pbr", "made-up-thing"]
}`, 1)
	m, findings := ParseManifest([]byte(src))
	if m == nil || HasErrors(findings) {
		t.Fatalf("capabilities must never be errors: %+v", findings)
	}
	if len(m.Capabilities) != 2 {
		t.Errorf("capabilities = %v", m.Capabilities)
	}
	warned := false
	for _, f := range findings {
		if strings.Contains(f.Msg, "made-up-thing") {
			warned = true
		}
	}
	if !warned {
		t.Error("expected unknown-capability warning")
	}
}
