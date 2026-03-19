package stream

import (
	"reflect"
	"testing"
)

func TestBuiltinTemplates(t *testing.T) {
	templates := BuiltinTemplates()
	if len(templates) == 0 {
		t.Fatal("expected at least one builtin template")
	}

	classic := templates[0]
	if classic.Name != "Classic" {
		t.Errorf("expected first template named Classic, got %q", classic.Name)
	}
	if classic.Description == "" {
		t.Error("expected non-empty description for Classic template")
	}

	// Verify Classic has the expected top-level phases.
	var names []string
	for _, p := range classic.Phases {
		names = append(names, p.Name)
	}
	want := []string{"research", "plan", "coding", "review", "polish"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("Classic top-level phases = %v, want %v", names, want)
	}

	// Verify plan has decompose as a child.
	var planNode PhaseNode
	for _, p := range classic.Phases {
		if p.Name == "plan" {
			planNode = p
			break
		}
	}
	if len(planNode.Children) != 1 || planNode.Children[0].Name != "decompose" {
		t.Errorf("expected plan to have [decompose] child, got %v", planNode.Children)
	}
}

func TestFindTemplate(t *testing.T) {
	templates := BuiltinTemplates()

	found := FindTemplate("Classic", templates)
	if found == nil {
		t.Fatal("expected to find Classic template")
	}
	if found.Name != "Classic" {
		t.Errorf("expected Classic, got %q", found.Name)
	}

	notFound := FindTemplate("Nonexistent", templates)
	if notFound != nil {
		t.Error("expected nil for nonexistent template")
	}

	empty := FindTemplate("Classic", nil)
	if empty != nil {
		t.Error("expected nil for empty template list")
	}
}

func TestParsePhaseTree(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []PhaseNode
	}{
		{
			name: "empty",
			spec: "",
			want: nil,
		},
		{
			name: "single phase",
			spec: "coding",
			want: []PhaseNode{{Name: "coding"}},
		},
		{
			name: "flat list",
			spec: "research,coding,review",
			want: []PhaseNode{
				{Name: "research"},
				{Name: "coding"},
				{Name: "review"},
			},
		},
		{
			name: "nested phase",
			spec: "research,plan>decompose,coding,review,polish",
			want: []PhaseNode{
				{Name: "research"},
				{Name: "plan", Children: []PhaseNode{{Name: "decompose"}}},
				{Name: "coding"},
				{Name: "review"},
				{Name: "polish"},
			},
		},
		{
			name: "whitespace handling",
			spec: " research , plan > decompose , coding ",
			want: []PhaseNode{
				{Name: "research"},
				{Name: "plan", Children: []PhaseNode{{Name: "decompose"}}},
				{Name: "coding"},
			},
		},
		{
			name: "multiple children via repeated parent",
			spec: "plan>decompose,plan>research,coding",
			want: []PhaseNode{
				{Name: "plan", Children: []PhaseNode{{Name: "decompose"}, {Name: "research"}}},
				{Name: "coding"},
			},
		},
		{
			name: "adjacent nesting appends child",
			spec: "plan>decompose,coding",
			want: []PhaseNode{
				{Name: "plan", Children: []PhaseNode{{Name: "decompose"}}},
				{Name: "coding"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePhaseTree(tt.spec)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParsePhaseTree(%q) =\n  %+v\nwant\n  %+v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestMergeTemplates(t *testing.T) {
	builtins := []Template{
		{Name: "Classic", Description: "Original", Phases: []PhaseNode{{Name: "coding"}}},
		{Name: "Minimal", Description: "Bare minimum", Phases: []PhaseNode{{Name: "coding"}}},
	}

	t.Run("no extras", func(t *testing.T) {
		merged := MergeTemplates(builtins, nil)
		if len(merged) != 2 {
			t.Fatalf("expected 2 templates, got %d", len(merged))
		}
	})

	t.Run("add new template", func(t *testing.T) {
		extras := []Template{
			{Name: "Iterative", Description: "With refine", Phases: []PhaseNode{{Name: "coding"}, {Name: "refine"}}},
		}
		merged := MergeTemplates(builtins, extras)
		if len(merged) != 3 {
			t.Fatalf("expected 3 templates, got %d", len(merged))
		}
		if merged[2].Name != "Iterative" {
			t.Errorf("expected Iterative appended, got %q", merged[2].Name)
		}
	})

	t.Run("override existing template", func(t *testing.T) {
		extras := []Template{
			{Name: "Classic", Description: "Overridden", Phases: []PhaseNode{{Name: "research"}, {Name: "coding"}}},
		}
		merged := MergeTemplates(builtins, extras)
		if len(merged) != 2 {
			t.Fatalf("expected 2 templates (override, not add), got %d", len(merged))
		}
		if merged[0].Description != "Overridden" {
			t.Errorf("expected overridden description, got %q", merged[0].Description)
		}
		if len(merged[0].Phases) != 2 {
			t.Errorf("expected 2 phases in overridden template, got %d", len(merged[0].Phases))
		}
	})

	t.Run("mix add and override", func(t *testing.T) {
		extras := []Template{
			{Name: "Classic", Description: "Overridden"},
			{Name: "New", Description: "Brand new"},
		}
		merged := MergeTemplates(builtins, extras)
		if len(merged) != 3 {
			t.Fatalf("expected 3 templates, got %d", len(merged))
		}
		if merged[0].Description != "Overridden" {
			t.Errorf("expected Classic overridden, got %q", merged[0].Description)
		}
		if merged[2].Name != "New" {
			t.Errorf("expected New appended, got %q", merged[2].Name)
		}
	})

	t.Run("does not mutate builtins", func(t *testing.T) {
		original := make([]Template, len(builtins))
		copy(original, builtins)
		extras := []Template{
			{Name: "Classic", Description: "Overridden"},
		}
		MergeTemplates(builtins, extras)
		if !reflect.DeepEqual(builtins, original) {
			t.Error("MergeTemplates mutated the builtins slice")
		}
	})
}
