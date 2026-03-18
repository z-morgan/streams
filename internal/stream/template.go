package stream

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
