package loop

import (
	"strings"

	"github.com/zmorgan/streams/internal/stream"
)

// Gate evaluates a quality criterion against review output.
type Gate interface {
	Name() string
	Evaluate(reviewText string) stream.GateResult
}

// keywordGate is a gate that fails when any of its keywords appear in the review text.
type keywordGate struct {
	name     string
	keywords []string
}

func (g *keywordGate) Name() string { return g.name }

func (g *keywordGate) Evaluate(reviewText string) stream.GateResult {
	lower := strings.ToLower(reviewText)
	for _, kw := range g.keywords {
		if strings.Contains(lower, kw) {
			return stream.GateResult{
				Gate:   g.name,
				Passed: false,
				Detail: "review mentions " + kw,
			}
		}
	}
	return stream.GateResult{
		Gate:   g.name,
		Passed: true,
		Detail: "no concerns found",
	}
}

// PatternConformanceGate checks if review text contains pattern/convention concerns.
func PatternConformanceGate() Gate {
	return &keywordGate{
		name:     "pattern-conformance",
		keywords: []string{"pattern", "convention", "inconsistent"},
	}
}

// SimplicityGate checks if review text mentions simplification needed.
func SimplicityGate() Gate {
	return &keywordGate{
		name:     "simplicity",
		keywords: []string{"simplify", "complex", "unnecessary", "remove"},
	}
}

// ReadabilityGate checks if review text mentions readability issues.
func ReadabilityGate() Gate {
	return &keywordGate{
		name:     "readability",
		keywords: []string{"readability", "unclear", "confusing", "hard to follow"},
	}
}

// HindsightGate checks if review text suggests the approach should be reconsidered.
func HindsightGate() Gate {
	return &keywordGate{
		name:     "hindsight",
		keywords: []string{"reconsider", "rethink", "wrong approach", "should have"},
	}
}

// DefaultGates returns the standard set of quality gates.
func DefaultGates() []Gate {
	return []Gate{
		PatternConformanceGate(),
		SimplicityGate(),
		ReadabilityGate(),
		HindsightGate(),
	}
}
