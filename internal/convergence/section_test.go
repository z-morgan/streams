package convergence

import "testing"

func TestDetectHeadingSections(t *testing.T) {
	content := `# Title

Some intro text.

## Step 0: Runtime verification

Content for step 0.

### Substep

More content.

## Step 1: Fix persistence bug

Content for step 1.
`
	sections := detectHeadingSections(content)
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}

	if sections[0].ID != "step-0-runtime-verification" {
		t.Errorf("section[0].ID = %q, want %q", sections[0].ID, "step-0-runtime-verification")
	}
	if sections[1].ID != "step-1-fix-persistence-bug" {
		t.Errorf("section[1].ID = %q, want %q", sections[1].ID, "step-1-fix-persistence-bug")
	}
}

func TestDetectHeadingSections_NoHeadings(t *testing.T) {
	content := "Just some plain text\nwith no headings.\n"
	sections := detectHeadingSections(content)
	if len(sections) != 1 {
		t.Fatalf("got %d sections, want 1", len(sections))
	}
	if sections[0].ID != "document" {
		t.Errorf("section[0].ID = %q, want %q", sections[0].ID, "document")
	}
}

func TestDetectHeadingSections_Empty(t *testing.T) {
	sections := detectHeadingSections("")
	if len(sections) != 0 {
		t.Fatalf("got %d sections for empty content, want 0", len(sections))
	}
}

func TestDetectFileSections(t *testing.T) {
	files := []string{"internal/loop/loop.go", "internal/stream/stream.go"}
	sections := detectFileSections(files)
	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	if sections[0].ID != "internal/loop/loop.go" {
		t.Errorf("section[0].ID = %q", sections[0].ID)
	}
}

func TestDetectSections_Auto(t *testing.T) {
	content := "## Heading\n\nContent\n"

	// Plan phase should use headings.
	sections := DetectSections(content, SectionDetectionAuto, "plan", nil)
	if len(sections) != 1 || sections[0].ID != "heading" {
		t.Errorf("plan phase: got %v, want heading section", sections)
	}

	// Coding phase should use files.
	files := []string{"main.go"}
	sections = DetectSections(content, SectionDetectionAuto, "coding", files)
	if len(sections) != 1 || sections[0].ID != "main.go" {
		t.Errorf("coding phase: got %v, want file section", sections)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Step 0: Runtime verification", "step-0-runtime-verification"},
		{"Hello World!", "hello-world"},
		{"  spaces  everywhere  ", "spaces-everywhere"},
		{"CamelCase", "camelcase"},
	}
	for _, tt := range tests {
		if got := slugify(tt.input); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
