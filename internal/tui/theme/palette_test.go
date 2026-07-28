package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestPalette_AuroraValues(t *testing.T) {
	tests := map[string]struct {
		got  lipgloss.Color
		want lipgloss.Color
	}{
		"border":          {Border, "#50617A"},
		"border strong":   {BorderStrong, "#8DAECC"},
		"primary":         {Primary, "#C5A6FF"},
		"text":            {Text, "#F1F6FF"},
		"text muted":      {TextMuted, "#A8B5CA"},
		"success":         {Success, "#68E6AE"},
		"warning":         {Warning, "#FFD27A"},
		"danger":          {Danger, "#FF8299"},
		"info":            {Info, "#75D8FF"},
		"glass highlight": {GlassHighlight, "#D7ECFF"},
		"glass shadow":    {GlassShadow, "#6B7D99"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("color = %q, want %q", test.got, test.want)
			}
		})
	}
}

func TestFocusedGlassBorder_UsesThickDirectionalEdges(t *testing.T) {
	style := FocusedGlassBorder(Primary)
	if style.GetBorderStyle() != lipgloss.ThickBorder() {
		t.Fatal("focused glass border must use thick glyphs")
	}
	if style.GetBorderTopForeground() != Primary || style.GetBorderLeftForeground() != Primary {
		t.Fatal("top and left edges must use focus color")
	}
	if style.GetBorderBottomForeground() != GlassShadow || style.GetBorderRightForeground() != GlassShadow {
		t.Fatal("bottom and right edges must use glass shadow")
	}
}

func TestGlassBorder_UsesDirectionalEdges(t *testing.T) {
	for name, highlight := range map[string]lipgloss.Color{
		"inactive": GlassHighlight,
		"focused":  Primary,
	} {
		t.Run(name, func(t *testing.T) {
			style := GlassBorder(highlight)
			if style.GetBorderTopForeground() != highlight || style.GetBorderLeftForeground() != highlight {
				t.Fatal("top and left edges must use highlight color")
			}
			if style.GetBorderBottomForeground() != GlassShadow || style.GetBorderRightForeground() != GlassShadow {
				t.Fatal("bottom and right edges must use glass shadow")
			}
		})
	}
}
