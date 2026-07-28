package tui

type layoutTier uint8

const (
	layoutNarrow layoutTier = iota
	layoutCompact
	layoutWide
)

type appLayout struct {
	tier            layoutTier
	gutter          int
	headerHeight    int
	mainHeight      int
	outputHeight    int
	footerHeight    int
	verticalGutters int
	tasksWidth      int
	rightWidth      int
}

func layoutTierForWidth(width int) layoutTier {
	if width >= 120 {
		return layoutWide
	}
	if width >= 80 {
		return layoutCompact
	}
	return layoutNarrow
}

func calculateLayout(width, height, preferredOutput int) appLayout {
	l := appLayout{tier: layoutTierForWidth(width)}
	if width <= 0 || height <= 0 {
		return l
	}

	l.headerHeight = min(3, height)
	if height < 12 {
		l.headerHeight = 1
	}
	remaining := max(0, height-l.headerHeight)
	l.footerHeight = min(1, remaining)
	remaining -= l.footerHeight
	wantedGutters := 3
	if l.tier == layoutNarrow {
		wantedGutters = 1
	}
	l.verticalGutters = min(wantedGutters, remaining)
	remaining -= l.verticalGutters

	if remaining >= 6 {
		l.outputHeight = min(max(3, preferredOutput), remaining-3)
	}
	if l.tier == layoutNarrow && l.outputHeight > 4 {
		l.outputHeight = 4
	}
	l.mainHeight = max(0, remaining-l.outputHeight)

	if l.tier == layoutNarrow {
		l.tasksWidth = width
		l.rightWidth = width
		return l
	}

	l.gutter = min(1, width)
	available := max(0, width-l.gutter)
	ratio := 34
	if l.tier == layoutWide {
		ratio = 29
	}
	l.tasksWidth = available * ratio / 100
	l.rightWidth = available - l.tasksWidth
	return l
}
