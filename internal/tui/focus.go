package tui

type FocusPanel int

const (
	FocusTasks FocusPanel = iota

	FocusServices
	FocusReleases
	FocusOutput
)

func (f FocusPanel) Next() FocusPanel {
	switch f {
	case FocusTasks:
		return FocusServices
	case FocusServices:
		return FocusReleases
	case FocusReleases:
		return FocusOutput
	default:
		return FocusTasks
	}
}

func (f FocusPanel) Prev() FocusPanel {
	switch f {
	case FocusTasks:
		return FocusOutput
	case FocusServices:
		return FocusTasks
	case FocusReleases:
		return FocusServices
	default:
		return FocusReleases
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
