package panels

import "strings"

// ProgressState is the per-service state of a tracked long-running operation.
type ProgressState int

const (
	ProgressPending ProgressState = iota
	ProgressRunning
	ProgressDone
	ProgressFailed
	ProgressSkipped
)

// ServiceProgress is the latest known progress of one service.
type ServiceProgress struct {
	State ProgressState
	Phase string
}

// OperationProgress is a snapshot of one tracked operation across services.
type OperationProgress struct {
	TaskID   string
	Op       string
	Services map[string]ServiceProgress
}

func NewOperationProgress(taskID, op string) *OperationProgress {
	return &OperationProgress{TaskID: taskID, Op: op, Services: make(map[string]ServiceProgress)}
}

// ApplyLine folds one streamed status line into the snapshot.
func (p *OperationProgress) ApplyLine(line string) {
	svc, phase, state, ok := ParseProgressLine(line)
	if !ok {
		return
	}
	p.Services[svc] = ServiceProgress{State: state, Phase: phase}
}

// Finish resolves services still running when the operation ends.
func (p *OperationProgress) Finish(err error) {
	for svc, sp := range p.Services {
		if sp.State != ProgressRunning {
			continue
		}
		if err != nil {
			p.Services[svc] = ServiceProgress{State: ProgressFailed, Phase: sp.Phase}
		} else {
			p.Services[svc] = ServiceProgress{State: ProgressDone, Phase: sp.Phase}
		}
	}
}

// SetService sets an explicit terminal state for a service (e.g. from typed results).
func (p *OperationProgress) SetService(name string, state ProgressState) {
	sp, ok := p.Services[name]
	if !ok {
		return
	}
	sp.State = state
	p.Services[name] = sp
}

// Counts tallies states over the given service order.
func (p *OperationProgress) Counts(order []string) (done, failed, running, skipped, pending int) {
	for _, name := range order {
		switch p.Services[name].State {
		case ProgressDone:
			done++
		case ProgressFailed:
			failed++
		case ProgressRunning:
			running++
		case ProgressSkipped:
			skipped++
		default:
			pending++
		}
	}
	return
}

// ParseProgressLine extracts service progress from a streamed status line.
// Supported formats: "[svc] fetching...", "[svc:phase] msg", "[svc][phase] msg".
// ponytail: status lines stay the contract; unknown lines are output-only.
func ParseProgressLine(line string) (service, phase string, state ProgressState, ok bool) {
	if !strings.HasPrefix(line, "[") {
		return "", "", 0, false
	}
	end := strings.Index(line, "]")
	if end <= 1 {
		return "", "", 0, false
	}
	head := line[1:end]
	rest := strings.TrimSpace(line[end+1:])

	// [svc][phase] rest
	if strings.HasPrefix(rest, "[") {
		e2 := strings.Index(rest, "]")
		if e2 > 1 {
			phase = rest[1:e2]
			rest = strings.TrimSpace(rest[e2+1:])
		}
	}
	service = head
	// [svc:phase] rest (close steps may nest "svc:merge:target")
	if i := strings.Index(service, ":"); i >= 0 {
		service, phase = head[:i], head[i+1:]
		if j := strings.Index(phase, ":"); j >= 0 {
			phase = phase[:j]
		}
	}
	if service == "" {
		return "", "", 0, false
	}

	if phase != "" {
		state = ProgressRunning
		if rest == "" || strings.HasPrefix(rest, "failed") || strings.Contains(rest, " error:") {
			state = ProgressFailed
		}
		return service, phase, state, true
	}

	phase, state, ok = classifyRest(rest)
	return service, phase, state, ok
}

func classifyRest(rest string) (string, ProgressState, bool) {
	if rest == "" {
		return "", 0, false
	}
	lower := strings.ToLower(rest)
	switch {
	case strings.HasPrefix(lower, "warning:"):
		return "", 0, false
	case strings.Contains(lower, " error:") || strings.HasPrefix(lower, "failed"):
		return firstWord(lower) + " error", ProgressFailed, true
	case strings.Contains(lower, "skipping") || strings.Contains(lower, "stash or commit first"):
		return "skipped", ProgressSkipped, true
	case lower == "done." || lower == "done":
		return "done", ProgressDone, true
	case lower == "pushed." || lower == "pushed":
		return "pushed", ProgressDone, true
	case strings.HasPrefix(lower, "already up to date"):
		return "up to date", ProgressDone, true
	case strings.HasPrefix(lower, "could not"):
		return "checking", ProgressRunning, true
	default:
		return firstWord(lower), ProgressRunning, true
	}
}

func firstWord(s string) string {
	word := strings.Fields(s)
	if len(word) == 0 {
		return "running"
	}
	return strings.TrimRight(word[0], ".:,")
}
