package tui

type FocusPanel int

const (
	FocusTasks FocusPanel = iota

	FocusServices
	FocusOutput
)

func (f FocusPanel) Next() FocusPanel {
	if f == FocusTasks {
		return FocusServices
	}
	return FocusTasks
}

func (f FocusPanel) Prev() FocusPanel {
	if f == FocusServices {
		return FocusTasks
	}
	return FocusServices
}

func (f FocusPanel) String() string {
	switch f {
	case FocusTasks:
		return "tasks"
	case FocusServices:
		return "services"
	case FocusOutput:
		return "output"
	default:
		return "unknown"
	}
}
