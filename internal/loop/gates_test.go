package loop

import (
	"testing"
)

func TestPatternConformanceGate_Passes(t *testing.T) {
	gate := PatternConformanceGate()
	result := gate.Evaluate("The code looks great and follows all best practices.")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

func TestPatternConformanceGate_FailsOnPattern(t *testing.T) {
	gate := PatternConformanceGate()
	result := gate.Evaluate("The code does not follow the expected Pattern here.")
	if result.Passed {
		t.Error("expected fail for 'pattern' keyword")
	}
}

func TestPatternConformanceGate_FailsOnConvention(t *testing.T) {
	gate := PatternConformanceGate()
	result := gate.Evaluate("This violates the naming convention.")
	if result.Passed {
		t.Error("expected fail for 'convention' keyword")
	}
}

func TestPatternConformanceGate_FailsOnInconsistent(t *testing.T) {
	gate := PatternConformanceGate()
	result := gate.Evaluate("The style is Inconsistent with the rest of the codebase.")
	if result.Passed {
		t.Error("expected fail for 'inconsistent' keyword")
	}
}

func TestSimplicityGate_Passes(t *testing.T) {
	gate := SimplicityGate()
	result := gate.Evaluate("The implementation is clean and well-structured.")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

func TestSimplicityGate_FailsOnSimplify(t *testing.T) {
	gate := SimplicityGate()
	result := gate.Evaluate("You should Simplify this logic.")
	if result.Passed {
		t.Error("expected fail for 'simplify' keyword")
	}
}

func TestSimplicityGate_FailsOnComplex(t *testing.T) {
	gate := SimplicityGate()
	result := gate.Evaluate("This is too Complex for the task.")
	if result.Passed {
		t.Error("expected fail for 'complex' keyword")
	}
}

func TestSimplicityGate_FailsOnUnnecessary(t *testing.T) {
	gate := SimplicityGate()
	result := gate.Evaluate("There is Unnecessary abstraction here.")
	if result.Passed {
		t.Error("expected fail for 'unnecessary' keyword")
	}
}

func TestSimplicityGate_FailsOnRemove(t *testing.T) {
	gate := SimplicityGate()
	result := gate.Evaluate("Please Remove the dead code.")
	if result.Passed {
		t.Error("expected fail for 'remove' keyword")
	}
}

func TestReadabilityGate_Passes(t *testing.T) {
	gate := ReadabilityGate()
	result := gate.Evaluate("The code is well-written and easy to understand.")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

func TestReadabilityGate_FailsOnReadability(t *testing.T) {
	gate := ReadabilityGate()
	result := gate.Evaluate("The Readability of this function is poor.")
	if result.Passed {
		t.Error("expected fail for 'readability' keyword")
	}
}

func TestReadabilityGate_FailsOnUnclear(t *testing.T) {
	gate := ReadabilityGate()
	result := gate.Evaluate("The intent is Unclear.")
	if result.Passed {
		t.Error("expected fail for 'unclear' keyword")
	}
}

func TestReadabilityGate_FailsOnConfusing(t *testing.T) {
	gate := ReadabilityGate()
	result := gate.Evaluate("This naming is Confusing.")
	if result.Passed {
		t.Error("expected fail for 'confusing' keyword")
	}
}

func TestReadabilityGate_FailsOnHardToFollow(t *testing.T) {
	gate := ReadabilityGate()
	result := gate.Evaluate("The logic is Hard to follow.")
	if result.Passed {
		t.Error("expected fail for 'hard to follow' keyword")
	}
}

func TestHindsightGate_Passes(t *testing.T) {
	gate := HindsightGate()
	result := gate.Evaluate("The approach is solid and well-chosen.")
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Detail)
	}
}

func TestHindsightGate_FailsOnReconsider(t *testing.T) {
	gate := HindsightGate()
	result := gate.Evaluate("We should Reconsider this approach.")
	if result.Passed {
		t.Error("expected fail for 'reconsider' keyword")
	}
}

func TestHindsightGate_FailsOnRethink(t *testing.T) {
	gate := HindsightGate()
	result := gate.Evaluate("We need to Rethink the design.")
	if result.Passed {
		t.Error("expected fail for 'rethink' keyword")
	}
}

func TestHindsightGate_FailsOnWrongApproach(t *testing.T) {
	gate := HindsightGate()
	result := gate.Evaluate("This is the Wrong approach for the problem.")
	if result.Passed {
		t.Error("expected fail for 'wrong approach' keyword")
	}
}

func TestHindsightGate_FailsOnShouldHave(t *testing.T) {
	gate := HindsightGate()
	result := gate.Evaluate("We Should have used a different strategy.")
	if result.Passed {
		t.Error("expected fail for 'should have' keyword")
	}
}

func TestDefaultGates_ReturnsFourGates(t *testing.T) {
	gates := DefaultGates()
	if len(gates) != 4 {
		t.Fatalf("expected 4 gates, got %d", len(gates))
	}

	expected := map[string]bool{
		"pattern-conformance": false,
		"simplicity":          false,
		"readability":         false,
		"hindsight":           false,
	}

	for _, g := range gates {
		if _, ok := expected[g.Name()]; !ok {
			t.Errorf("unexpected gate: %s", g.Name())
		}
		expected[g.Name()] = true
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing gate: %s", name)
		}
	}
}
