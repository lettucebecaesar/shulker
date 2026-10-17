package pack

import (
	"fmt"
	"regexp"

	"github.com/lettucebecaesar/shulker/internal/jsonx"
)

// uuidPattern is the canonical 8-4-4-4-12 hex form. We accept any UUID
// *variant/version* — packs in the wild use v4, and the engine accepts all.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Manifest is a parsed manifest.json. The raw bytes are kept because the
// manifest is stored VERBATIM in every output — UUIDs, versions, and
// min_engine_version may be coupled to invisible behavior packs, so shulker
// never re-serializes this file, only ever copies it.
type Manifest struct {
	Raw              []byte
	Root             jsonx.Value
	FormatVersionRaw string
	Name             string
	UUID             string
	MinEngineVersion []int64
	Modules          []ManifestModule
	Dependencies     []ManifestDependency
	Capabilities     []string
}

// ManifestModule is one entry of the modules array ("resources" is what RP
// modules use).
type ManifestModule struct {
	Type        string
	UUID        string
	Description string
}

// ManifestDependency is one entry of dependencies: either UUID+version
// (pack-to-pack linkage — the reason manifests are sacred) or
// module_name+version (script module linkage).
type ManifestDependency struct {
	UUID      string
	Version   string
	HasUUID   bool
	Module    string
	ModuleVer string
}

// knownCapabilities keeps validation from whining about capability names the
// engine supports. Drift here is a warning at worst, never an error.
var knownCapabilities = map[string]bool{
	"chemistry":   true,
	"script_eval": true,
	"pbr":         true, // Vibrant Visuals
	"raytraced":   true, // RTX-era
	"executables": true,
}

// ParseManifest decodes and validates manifest.json. The returned findings
// carry severity: errors mean the pack cannot load in-game and shulker
// refuses to process it; warnings are informational.
//
// MCBE quirk: format_version is a *number* (2) in every vanilla pack. The
// docs' example of format_version 1 still exists in legacy packs. Version
// fields are [major, minor, patch] arrays; newer packs also accept semver
// strings — both are tolerated, nothing is ever rewritten.
func ParseManifest(raw []byte) (*Manifest, []Finding) {
	root, err := jsonx.Parse(raw)
	if err != nil {
		return nil, []Finding{{
			Severity: SeverityError,
			Path:     "manifest.json",
			Msg:      "unparseable JSON: " + err.Error(),
		}}
	}
	m := &Manifest{Raw: raw, Root: root}
	var findings []Finding
	add := func(sev Severity, format string, args ...any) {
		findings = append(findings, Finding{Severity: sev, Path: "manifest.json", Msg: fmt.Sprintf(format, args...)})
	}

	obj, ok := jsonx.AsObject(root)
	if !ok {
		return nil, []Finding{{
			Severity: SeverityError,
			Path:     "manifest.json",
			Msg:      "top level must be an object",
		}}
	}

	// format_version
	if fv, ok := obj.Get("format_version"); ok {
		switch t := fv.(type) {
		case *jsonx.Number:
			m.FormatVersionRaw = t.Raw
		case *jsonx.String:
			m.FormatVersionRaw = t.Str
		default:
			add(SeverityError, "format_version must be a number (2) or string")
		}
	} else {
		add(SeverityError, "missing format_version")
	}
	switch m.FormatVersionRaw {
	case "2":
		// The one true modern value.
	case "1":
		add(SeverityWarning, "format_version 1 is legacy; modern packs use 2")
	case "":
	default:
		add(SeverityWarning, "unrecognized format_version %q (expected 2)", m.FormatVersionRaw)
	}

	// header
	headerV, ok := obj.Get("header")
	if !ok {
		add(SeverityError, "missing header")
		return m, findings
	}
	header, ok := jsonx.AsObject(headerV)
	if !ok {
		add(SeverityError, "header must be an object")
		return m, findings
	}
	if v, ok := header.Get("name"); ok {
		if s, isStr := jsonx.AsString(v); isStr {
			m.Name = s
		}
	}
	if m.Name == "" {
		add(SeverityWarning, "header.name missing or not a string")
	}
	if v, ok := header.Get("uuid"); ok {
		if s, isStr := jsonx.AsString(v); isStr {
			m.UUID = s
			if !uuidPattern.MatchString(s) {
				add(SeverityError, "header.uuid %q is not a UUID", s)
			}
		} else {
			add(SeverityError, "missing header.uuid — the pack cannot be identified")
		}
	} else {
		add(SeverityError, "missing header.uuid — the pack cannot be identified")
	}
	if v, ok := header.Get("min_engine_version"); ok {
		if parts, ok := parseIntArray(v); ok && len(parts) == 3 {
			m.MinEngineVersion = parts
		} else {
			add(SeverityWarning, "header.min_engine_version should be [major, minor, patch]")
		}
	} else {
		add(SeverityWarning, "missing header.min_engine_version; the engine assumes an ancient version")
	}

	// modules
	modulesV, ok := obj.Get("modules")
	if !ok {
		add(SeverityError, "missing modules — a pack with no modules loads as nothing")
		return m, findings
	}
	modules, ok := jsonx.AsArray(modulesV)
	if !ok || len(modules.Items) == 0 {
		add(SeverityError, "modules must be a non-empty array")
	} else {
		for i, item := range modules.Items {
			mo, ok := jsonx.AsObject(item)
			if !ok {
				add(SeverityError, "modules[%d] must be an object", i)
				continue
			}
			mm := ManifestModule{}
			if v, ok := mo.Get("type"); ok {
				if s, isStr := jsonx.AsString(v); isStr {
					mm.Type = s
				}
			}
			if mm.Type == "" {
				add(SeverityError, "modules[%d].type missing", i)
			}
			if v, ok := mo.Get("uuid"); ok {
				if s, isStr := jsonx.AsString(v); isStr {
					mm.UUID = s
					if !uuidPattern.MatchString(s) {
						add(SeverityError, "modules[%d].uuid %q is not a UUID", i, s)
					}
					if s == m.UUID {
						add(SeverityError, "modules[%d].uuid equals header.uuid (must be unique)", i)
					}
				} else {
					add(SeverityError, "modules[%d].uuid missing", i)
				}
			} else {
				add(SeverityError, "modules[%d].uuid missing", i)
			}
			if v, ok := mo.Get("description"); ok {
				if s, isStr := jsonx.AsString(v); isStr {
					mm.Description = s
				}
			}
			if _, ok := mo.Get("version"); !ok {
				add(SeverityWarning, "modules[%d].version missing", i)
			}
			m.Modules = append(m.Modules, mm)
		}
	}

	// dependencies — validated loosely; their shape is engine business.
	if depsV, ok := obj.Get("dependencies"); ok {
		if deps, ok := jsonx.AsArray(depsV); ok {
			for _, item := range deps.Items {
				o, ok := jsonx.AsObject(item)
				if !ok {
					continue
				}
				dep := ManifestDependency{}
				if v, ok := o.Get("uuid"); ok {
					if s, isStr := jsonx.AsString(v); isStr {
						dep.UUID = s
						dep.HasUUID = true
						if !uuidPattern.MatchString(s) {
							add(SeverityWarning, "dependency uuid %q is not a UUID", s)
						}
					}
				}
				if v, ok := o.Get("module_name"); ok {
					if s, isStr := jsonx.AsString(v); isStr {
						dep.Module = s
					}
				}
				m.Dependencies = append(m.Dependencies, dep)
			}
		} else {
			add(SeverityWarning, "dependencies should be an array")
		}
	}

	// capabilities
	if capsV, ok := obj.Get("capabilities"); ok {
		if caps, ok := jsonx.AsArray(capsV); ok {
			for _, item := range caps.Items {
				s, ok := jsonx.AsString(item)
				if !ok {
					add(SeverityWarning, "capabilities entries must be strings")
					continue
				}
				if !knownCapabilities[s] {
					add(SeverityWarning, "unknown capability %q (may be newer than shulker)", s)
				}
				m.Capabilities = append(m.Capabilities, s)
			}
		} else {
			add(SeverityWarning, "capabilities should be an array")
		}
	}

	return m, findings
}

// HasErrors reports whether any finding is an error.
func HasErrors(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == SeverityError {
			return true
		}
	}
	return false
}

func parseIntArray(v jsonx.Value) ([]int64, bool) {
	arr, ok := jsonx.AsArray(v)
	if !ok {
		return nil, false
	}
	var out []int64
	for _, item := range arr.Items {
		raw, ok := jsonx.AsNumber(item)
		if !ok {
			return nil, false
		}
		var n int64
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}
