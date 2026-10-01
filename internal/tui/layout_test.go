package tui

import "testing"

func TestLayoutTierForWidth(t *testing.T) {
	tests := []struct {
		width int
		want  layoutTier
	}{
		{width: 79, want: layoutNarrow},
		{width: 80, want: layoutCompact},
		{width: 119, want: layoutCompact},
		{width: 120, want: layoutWide},
	}

	for _, tt := range tests {
		if got := layoutTierForWidth(tt.width); got != tt.want {
			t.Fatalf("layoutTierForWidth(%d) = %v, want %v", tt.width, got, tt.want)
		}
	}
}

func TestCalculateLayout_WideUsesGuttersAndReferenceRatio(t *testing.T) {
	got := calculateLayout(160, 40, 8, 0)
	if got.tier != layoutWide || got.gutter != 1 {
		t.Fatalf("layout = %#v", got)
	}
	if got.tasksWidth+got.rightWidth+got.gutter != 160 {
		t.Fatalf("widths = %#v", got)
	}
	if got.headerHeight+got.workflowHeight+got.mainHeight+got.outputHeight+got.footerHeight+got.verticalGutters != 40 {
		t.Fatalf("heights = %#v", got)
	}
	if got.tasksWidth != 46 || got.rightWidth != 113 {
		t.Fatalf("panel widths = %d/%d, want 46/113", got.tasksWidth, got.rightWidth)
	}
}

func TestCalculateLayout_WorkflowHeightDeductedFromMainArea(t *testing.T) {
	without := calculateLayout(120, 40, 8, 0)
	with := calculateLayout(120, 40, 8, 3)
	if with.workflowHeight != 3 {
		t.Fatalf("workflowHeight = %d, want 3", with.workflowHeight)
	}
	if with.mainHeight != without.mainHeight-3 {
		t.Fatalf("mainHeight = %d, want %d", with.mainHeight, without.mainHeight-3)
	}
	if with.outputHeight != without.outputHeight {
		t.Fatalf("output height must be unaffected: %d vs %d", with.outputHeight, without.outputHeight)
	}
}

func TestCalculateLayout_WorkflowHeightCappedByAvailableSpace(t *testing.T) {
	got := calculateLayout(120, 8, 8, 20)
	if got.workflowHeight > 8 {
		t.Fatalf("workflowHeight = %d exceeds terminal height", got.workflowHeight)
	}
	total := got.headerHeight + got.workflowHeight + got.mainHeight + got.outputHeight + got.footerHeight + got.verticalGutters
	if total > 8 {
		t.Fatalf("layout overflows: total = %d", total)
	}
}

func TestCalculateLayout_NeverReturnsNegativeDimensions(t *testing.T) {
	for width := 0; width < 12; width++ {
		for height := 0; height < 12; height++ {
			got := calculateLayout(width, height, 8, 4)
			for name, value := range map[string]int{
				"header":   got.headerHeight,
				"workflow": got.workflowHeight,
				"main":     got.mainHeight,
				"output":   got.outputHeight,
				"footer":   got.footerHeight,
				"tasks":    got.tasksWidth,
				"right":    got.rightWidth,
			} {
				if value < 0 {
					t.Fatalf("calculateLayout(%d, %d) %s = %d", width, height, name, value)
				}
			}
			total := got.headerHeight + got.workflowHeight + got.mainHeight + got.outputHeight + got.footerHeight + got.verticalGutters
			if total > height {
				t.Fatalf("calculateLayout(%d, %d) overflows: total = %d", width, height, total)
			}
		}
	}
}
