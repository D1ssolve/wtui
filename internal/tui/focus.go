package tui

// FocusPanel represents which panel currently has keyboard focus.
// The zero value (0) is Tasks, which is the default focus on startup.
type FocusPanel int

const (
	// FocusTasks is the left panel showing the list of task groups.
	FocusTasks FocusPanel = iota
	// FocusServices is the right panel showing services for the selected task.
	// NOTE: FocusServices is NOT part of the Tab cycle — it is entered only via
	// Enter on a task (FocusServicesMsg) and exited via Esc.
	FocusServices
	// FocusOutput is the bottom panel showing subprocess log output.
	FocusOutput
)

// Next returns the next FocusPanel in the Tab cycle.
// FocusServices is intentionally excluded from Tab cycling — it is reached
// only via Enter on a task (FocusServicesMsg).
func (f FocusPanel) Next() FocusPanel {
	if f == FocusTasks {
		return FocusOutput
	}
	return FocusTasks // FocusOutput → Tasks; FocusServices or any other → Tasks
}

// Prev returns the previous FocusPanel in the Shift+Tab cycle.
func (f FocusPanel) Prev() FocusPanel {
	if f == FocusTasks {
		return FocusOutput
	}
	return FocusTasks // FocusOutput → Tasks; FocusServices or any other → Tasks
}

// String returns a human-readable name for the focused panel (useful for logging).
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
