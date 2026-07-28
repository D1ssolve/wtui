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
	got := calculateLayout(160, 40, 8)
	if got.tier != layoutWide || got.gutter != 1 {
		t.Fatalf("layout = %#v", got)
	}
	if got.tasksWidth+got.rightWidth+got.gutter != 160 {
		t.Fatalf("widths = %#v", got)
	}
	if got.headerHeight+got.mainHeight+got.outputHeight+got.footerHeight+got.verticalGutters != 40 {
		t.Fatalf("heights = %#v", got)
	}
	if got.tasksWidth != 46 || got.rightWidth != 113 {
		t.Fatalf("panel widths = %d/%d, want 46/113", got.tasksWidth, got.rightWidth)
	}
}

func TestCalculateLayout_NeverReturnsNegativeDimensions(t *testing.T) {
	for width := 0; width < 12; width++ {
		for height := 0; height < 12; height++ {
			got := calculateLayout(width, height, 8)
			for name, value := range map[string]int{
				"header": got.headerHeight,
				"main":   got.mainHeight,
				"output": got.outputHeight,
				"footer": got.footerHeight,
				"tasks":  got.tasksWidth,
				"right":  got.rightWidth,
			} {
				if value < 0 {
					t.Fatalf("calculateLayout(%d, %d) %s = %d", width, height, name, value)
				}
			}
		}
	}
}
