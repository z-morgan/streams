package stream

import "strings"

// PhaseNode defines a phase that can be selected when creating a stream.
// Children are nested under their parent (e.g., decompose under plan).
type PhaseNode struct {
	Name     string      `json:"name"`
	Children []PhaseNode `json:"children,omitempty"`
}

// Template defines a named phase configuration for stream creation.
type Template struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Phases      []PhaseNode `json:"phases"`
}

// BuiltinTemplates returns the default set of templates.
func BuiltinTemplates() []Template {
	return []Template{
		{
			Name:        "Classic",
			Description: "Research, plan, code, review, polish",
			Phases: []PhaseNode{
				{Name: "research"},
				{Name: "plan", Children: []PhaseNode{
					{Name: "decompose"},
				}},
				{Name: "coding"},
				{Name: "review"},
				{Name: "polish"},
			},
		},
		{
			Name:        "Incremental",
			Description: "Step-by-step coding with inline review and refinement",
			Phases: []PhaseNode{
				{Name: "research"},
				{Name: "plan", Children: []PhaseNode{
					{Name: "decompose"},
				}},
				{Name: "step-coding"},
				{Name: "refine"},
				{Name: "review"},
				{Name: "polish"},
			},
		},
	}
}

// FindTemplate returns the template with the given name, or nil if not found.
func FindTemplate(name string, templates []Template) *Template {
	for i := range templates {
		if templates[i].Name == name {
			return &templates[i]
		}
	}
	return nil
}

// ParsePhaseTree converts a compact phase specification string into a phase tree.
// Phases are comma-separated. The ">" operator nests the next phase under the
// previous one (e.g., "research,plan>decompose,coding" produces decompose as a
// child of plan).
func ParsePhaseTree(spec string) []PhaseNode {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	var nodes []PhaseNode
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx := strings.Index(part, ">"); idx >= 0 {
			parent := strings.TrimSpace(part[:idx])
			child := strings.TrimSpace(part[idx+1:])
			if parent == "" || child == "" {
				continue
			}
			// Attach child to existing parent if it's the last node.
			if len(nodes) > 0 && nodes[len(nodes)-1].Name == parent {
				nodes[len(nodes)-1].Children = append(nodes[len(nodes)-1].Children, PhaseNode{Name: child})
			} else {
				nodes = append(nodes, PhaseNode{
					Name:     parent,
					Children: []PhaseNode{{Name: child}},
				})
			}
		} else {
			nodes = append(nodes, PhaseNode{Name: part})
		}
	}
	return nodes
}

// MergeTemplates combines built-in templates with config-defined templates.
// Config templates with the same name as a built-in override the built-in;
// new names are appended.
func MergeTemplates(builtins, extras []Template) []Template {
	merged := make([]Template, len(builtins))
	copy(merged, builtins)

	for _, extra := range extras {
		found := false
		for i := range merged {
			if merged[i].Name == extra.Name {
				merged[i] = extra
				found = true
				break
			}
		}
		if !found {
			merged = append(merged, extra)
		}
	}
	return merged
}
