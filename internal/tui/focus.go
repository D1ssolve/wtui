package tui

type FocusPanel int

const (
	FocusTasks FocusPanel = iota

	FocusServices
	FocusOutput
	FocusReleases
)

func (f FocusPanel) Next() FocusPanel {
	switch f {
	case FocusTasks:
		return FocusServices
	case FocusServices:
		return FocusOutput
	case FocusOutput:
		return FocusReleases
	default:
		return FocusTasks
	}
}

func (f FocusPanel) Prev() FocusPanel {
	switch f {
	case FocusTasks:
		return FocusReleases
	case FocusServices:
		return FocusTasks
	case FocusOutput:
		return FocusServices
	default:
		return FocusOutput
	}
}

func (f FocusPanel) String() string {
	switch f {
	case FocusTasks:
		return "tasks"
	case FocusServices:
		return "services"
	case FocusOutput:
		return "output"
	case FocusReleases:
		return "releases"
	default:
		return "unknown"
	}
}
