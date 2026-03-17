package ui

import (
	"strings"
	"testing"
)

// keyAliases maps arrow keys to their letter equivalents. These are handled
// in the same case branches (e.g., case "j", "down":) and don't need
// separate Bindings entries.
var keyAliases = map[string]string{
	"down":  "j",
	"up":    "k",
	"left":  "h",
	"right": "l",
}

// handledKeys lists every key string handled in each handler function.
// Arrow-key aliases are included here because they appear in the code,
// but the test resolves them to their primary key before checking.
//
// "dashboard" covers both updateDashboard (which handles list and channels
// mode keys in one function), so we check against ScopeDashboard + ScopeDashboardChannels.
var handledKeys = map[string]struct {
	scopes []Scope
	keys   []string
}{
	"dashboard": {
		scopes: []Scope{ScopeDashboard, ScopeDashboardChannels},
		keys: []string{
			"j", "k", "down", "up",
			"enter",
			"n",
			"s",
			"x",
			"X",
			"d",
			"D",
			"g",
			"v",
			"h", "l", "left", "right",
			"q",
		},
	},
	"detail": {
		scopes: []Scope{ScopeDetail},
		keys: []string{
			"j", "k", "down", "up",
			"enter",
			"esc", "q",
			"s",
			"c",
			"x",
			"X",
			"g",
			"f",
			"b",
			"d",
			"r",
			"w",
			">",
			"a",
			"D",
		},
	},
	"detailOutput": {
		scopes: []Scope{ScopeDetailOutput},
		keys: []string{
			"j", "k", "down", "up",
			"G",
			"f",
			"esc",
		},
	},
	"beadBrowse": {
		scopes: []Scope{ScopeBeadBrowse},
		keys: []string{
			"j", "k", "down", "up",
			"enter",
			"esc",
		},
	},
	"global": {
		scopes: []Scope{ScopeGlobal},
		keys:   []string{"?", "ctrl+c"},
	},
}

// expandKey splits a binding's display key into individual key strings.
// "j/k" → ["j","k"], "q/esc" → ["q","esc"], "ctrl+c" → ["ctrl+c"].
func expandKey(display string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(display); i++ {
		if display[i] == '/' {
			parts = append(parts, display[start:i])
			start = i + 1
		}
	}
	parts = append(parts, display[start:])
	return parts
}

// resolveAlias returns the primary key for an alias, or the key itself.
func resolveAlias(key string) string {
	if primary, ok := keyAliases[key]; ok {
		return primary
	}
	return key
}

func TestBindingsCoverage(t *testing.T) {
	for handler, spec := range handledKeys {
		// Build set of key strings from registry bindings across all scopes.
		registryKeys := make(map[string]bool)
		for _, scope := range spec.scopes {
			for _, b := range BindingsForScope(scope) {
				for _, k := range expandKey(b.Key) {
					registryKeys[k] = true
				}
			}
		}

		// Every handled key (resolved to primary) must have a registry entry.
		for _, k := range spec.keys {
			primary := resolveAlias(k)
			if !registryKeys[primary] {
				t.Errorf("handler %s: key %q (primary %q) is handled in code but missing from Bindings", handler, k, primary)
			}
		}

		// Every registry key must appear in the handled keys fixture (after resolving aliases).
		handledSet := make(map[string]bool)
		for _, k := range spec.keys {
			handledSet[resolveAlias(k)] = true
		}
		for _, scope := range spec.scopes {
			for _, b := range BindingsForScope(scope) {
				for _, k := range expandKey(b.Key) {
					if !handledSet[k] {
						t.Errorf("handler %s: key %q (from binding %q, scope %s) is in Bindings but not in handledKeys fixture",
							handler, k, b.Key, scope.Label())
					}
				}
			}
		}
	}
}

func TestAllBindingsHaveDescription(t *testing.T) {
	for i, b := range Bindings {
		if b.Description == "" {
			t.Errorf("Bindings[%d] (key=%q, scope=%s) has empty Description", i, b.Key, b.Scope.Label())
		}
	}
}

func TestHelpBarTextIncludesGlobalBindings(t *testing.T) {
	text := HelpBarText(ScopeDashboard, nil)
	if !strings.Contains(text, "?: this help") {
		t.Errorf("HelpBarText(ScopeDashboard) should include global ? binding, got: %s", text)
	}
	if !strings.Contains(text, "ctrl+c: quit") {
		t.Errorf("HelpBarText(ScopeDashboard) should include global ctrl+c binding, got: %s", text)
	}

	// Global scope alone should not duplicate.
	globalText := HelpBarText(ScopeGlobal, nil)
	count := strings.Count(globalText, "?")
	if count != 1 {
		t.Errorf("HelpBarText(ScopeGlobal) should contain ? exactly once, got %d in: %s", count, globalText)
	}
}

func TestExpandKey(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"j/k", []string{"j", "k"}},
		{"q/esc", []string{"q", "esc"}},
		{"ctrl+c", []string{"ctrl+c"}},
		{">", []string{">"}},
		{"h/l", []string{"h", "l"}},
	}
	for _, tt := range tests {
		got := expandKey(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("expandKey(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("expandKey(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
