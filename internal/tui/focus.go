package tui

type FocusPanel int

const (
	FocusTasks FocusPanel = iota
	// FocusServices is not part of the Tab/Shift-Tab cycle.
	// It is set exclusively via FocusServicesMsg dispatched from the tasks panel
	// when the user presses Enter on a task.
	FocusServices
	FocusOutput
)

func (f FocusPanel) Next() FocusPanel {
	if f == FocusTasks {
		return FocusOutput
	}
	return FocusTasks
}

func (f FocusPanel) Prev() FocusPanel {
	if f == FocusTasks {
		return FocusOutput
	}
	return FocusTasks
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
