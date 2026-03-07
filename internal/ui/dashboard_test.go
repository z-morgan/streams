package ui

import (
	"strings"
	"testing"

	"github.com/zmorgan/streams/internal/stream"
)

func TestChannelLayout(t *testing.T) {
	tests := []struct {
		name        string
		streamCount int
		termWidth   int
		wantWidth   int
		wantVisible int
	}{
		{"single stream wide term", 1, 120, 40, 1},
		{"two streams wide term", 2, 120, 40, 2},
		{"three streams wide term", 3, 120, 40, 3},
		{"four streams exact fit", 4, 100, 25, 4},
		{"more streams than fit", 6, 100, 25, 4},
		{"narrow terminal", 3, 30, 30, 1},
		{"very narrow terminal", 3, 20, 20, 1},
		{"zero streams", 0, 120, 40, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWidth, gotVisible := channelLayout(tt.streamCount, tt.termWidth)
			// Zero streams is a special case — visibleCols can't exceed streamCount
			if tt.streamCount == 0 {
				if gotVisible != 0 {
					t.Errorf("visibleCols = %d, want 0 for zero streams", gotVisible)
				}
				return
			}
			if gotWidth != tt.wantWidth {
				t.Errorf("colWidth = %d, want %d", gotWidth, tt.wantWidth)
			}
			if gotVisible != tt.wantVisible {
				t.Errorf("visibleCols = %d, want %d", gotVisible, tt.wantVisible)
			}
		})
	}
}

func TestClampScroll(t *testing.T) {
	tests := []struct {
		name        string
		scrollLeft  int
		streamCount int
		visibleCols int
		want        int
	}{
		{"no scroll needed", 0, 3, 3, 0},
		{"scroll at max", 2, 5, 3, 2},
		{"scroll past max", 5, 5, 3, 2},
		{"negative scroll", -1, 5, 3, 0},
		{"more visible than streams", 0, 2, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &dashboardView{scrollLeft: tt.scrollLeft}
			d.clampScroll(tt.streamCount, tt.visibleCols)
			if d.scrollLeft != tt.want {
				t.Errorf("scrollLeft = %d, want %d", d.scrollLeft, tt.want)
			}
		})
	}
}

func makeStream(name string, status stream.Status, pipeline []string, pipelineIdx int, snapshots []stream.Snapshot) *stream.Stream {
	return &stream.Stream{
		ID:            name,
		Name:          name,
		Status:        status,
		Pipeline:      pipeline,
		PipelineIndex: pipelineIdx,
		Snapshots:     snapshots,
	}
}

func TestRenderChannel(t *testing.T) {
	t.Run("basic stream with snapshots", func(t *testing.T) {
		st := makeStream("auth-refactor", stream.StatusPaused, []string{"plan", "coding"}, 1, []stream.Snapshot{
			{Phase: "plan", Iteration: 1, Summary: "Outlined approach"},
			{Phase: "coding", Iteration: 1, Summary: "Added basic auth"},
		})

		result := renderChannel(st, 30, 20, false)

		if !strings.Contains(result, "auth-refactor") {
			t.Error("expected stream name in output")
		}
		if !strings.Contains(result, "plan 1") {
			t.Error("expected snapshot row for plan 1")
		}
		if !strings.Contains(result, "coding 1") {
			t.Error("expected snapshot row for coding 1")
		}
	})

	t.Run("error snapshot shows bang prefix", func(t *testing.T) {
		st := makeStream("buggy", stream.StatusPaused, []string{"coding"}, 0, []stream.Snapshot{
			{Phase: "coding", Iteration: 1, Summary: "Hit error", Error: &stream.LoopError{Message: "fail"}},
		})

		result := renderChannel(st, 30, 20, false)

		if !strings.Contains(result, "!") {
			t.Error("expected error indicator '!' in output")
		}
	})

	t.Run("running stream shows in-progress", func(t *testing.T) {
		st := makeStream("active", stream.StatusRunning, []string{"coding"}, 0, nil)
		st.Iteration = 1

		result := renderChannel(st, 30, 20, false)

		if !strings.Contains(result, "> coding 1") {
			t.Errorf("expected in-progress indicator, got:\n%s", result)
		}
	})

	t.Run("vertical auto-scroll truncates old rows", func(t *testing.T) {
		var snaps []stream.Snapshot
		for i := 1; i <= 20; i++ {
			snaps = append(snaps, stream.Snapshot{Phase: "coding", Iteration: i, Summary: "work"})
		}
		st := makeStream("long", stream.StatusPaused, []string{"coding"}, 0, snaps)

		// availHeight=8 means maxRows=5, should only see last 5
		result := renderChannel(st, 30, 8, false)

		if strings.Contains(result, "coding 1:") {
			t.Error("expected early snapshots to be truncated")
		}
		if !strings.Contains(result, "coding 20") {
			t.Error("expected most recent snapshot to be visible")
		}
	})
}

func TestFlattenPhaseTree(t *testing.T) {
	flat := flattenPhaseTree(phaseTree, 0)

	if len(flat) != 4 {
		t.Fatalf("expected 4 flat phases, got %d", len(flat))
	}

	if flat[0].Name != "research" || flat[0].Depth != 0 {
		t.Errorf("flat[0] = %+v, want research depth 0", flat[0])
	}
	if flat[1].Name != "plan" || flat[1].Depth != 0 {
		t.Errorf("flat[1] = %+v, want plan depth 0", flat[1])
	}
	if flat[2].Name != "decompose" || flat[2].Depth != 1 {
		t.Errorf("flat[2] = %+v, want decompose depth 1", flat[2])
	}
	if flat[3].Name != "coding" || flat[3].Depth != 0 {
		t.Errorf("flat[3] = %+v, want coding depth 0", flat[3])
	}
}

func TestChildPhases(t *testing.T) {
	children := childPhases(phaseTree, "research")
	if len(children) != 0 {
		t.Errorf("childPhases(research) = %v, want []", children)
	}

	children = childPhases(phaseTree, "plan")
	if len(children) != 1 || children[0] != "decompose" {
		t.Errorf("childPhases(plan) = %v, want [decompose]", children)
	}

	children = childPhases(phaseTree, "coding")
	if len(children) != 0 {
		t.Errorf("childPhases(coding) = %v, want []", children)
	}

	children = childPhases(phaseTree, "nonexistent")
	if children != nil {
		t.Errorf("childPhases(nonexistent) = %v, want nil", children)
	}
}

func TestSelectedPipeline(t *testing.T) {
	tests := []struct {
		name    string
		checked map[string]bool
		want    string
	}{
		{"all checked", map[string]bool{"research": true, "plan": true, "decompose": true, "coding": true}, "research,plan,decompose,coding"},
		{"plan without decompose", map[string]bool{"plan": true, "coding": true}, "plan,coding"},
		{"research and code", map[string]bool{"research": true, "coding": true}, "research,coding"},
		{"code only", map[string]bool{"coding": true}, "coding"},
		{"none checked", map[string]bool{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectedPipeline(tt.checked, phaseTree)
			result := strings.Join(got, ",")
			if result != tt.want {
				t.Errorf("selectedPipeline = %q, want %q", result, tt.want)
			}
		})
	}
}

func TestDefaultPhaseChecks(t *testing.T) {
	checked := defaultPhaseChecks([]string{"plan", "coding"})
	if !checked["plan"] || !checked["coding"] {
		t.Error("expected plan and coding to be checked")
	}
	if checked["decompose"] {
		t.Error("expected decompose to be unchecked")
	}
}

func TestRenderChannels(t *testing.T) {
	t.Run("empty streams", func(t *testing.T) {
		result := renderChannels(nil, 0, 0, 120, 40)
		if !strings.Contains(result, "No streams yet") {
			t.Error("expected empty state message")
		}
	})

	t.Run("scroll indicators when more columns off-screen", func(t *testing.T) {
		streams := []*stream.Stream{
			makeStream("a", stream.StatusCreated, []string{"plan"}, 0, nil),
			makeStream("b", stream.StatusCreated, []string{"plan"}, 0, nil),
			makeStream("c", stream.StatusCreated, []string{"plan"}, 0, nil),
			makeStream("d", stream.StatusCreated, []string{"plan"}, 0, nil),
			makeStream("e", stream.StatusCreated, []string{"plan"}, 0, nil),
		}

		// Width 60 fits ~2 columns at min 25 width
		result := renderChannels(streams, 0, 0, 60, 30)
		if !strings.Contains(result, ">") {
			t.Error("expected right scroll indicator")
		}

		result = renderChannels(streams, 3, 2, 60, 30)
		if !strings.Contains(result, "<") {
			t.Error("expected left scroll indicator")
		}
	})
}
