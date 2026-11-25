package gitflow

// CancellablePath follows automatic transitions only. Hooks and reads may be
// interrupted; built-in Git/forge writes cannot be interrupted safely.
func (m *Machine) CancellablePath(start string) bool {
	seen := make(map[string]bool)
	queue := []string{start}
	for len(queue) > 0 {
		id := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		state, ok := m.State(id)
		if !ok {
			return false
		}
		switch state.Kind {
		case StateRun:
			for _, step := range state.Run {
				if step.Kind != StepHook {
					return false
				}
			}
			queue = append(queue, state.Next)
			if state.OnError != "" {
				queue = append(queue, state.OnError)
			}
		case StateWait:
			if state.Wait != WaitManual {
				queue = append(queue, state.Next)
			}
		case StateFinal:
		default:
			return false
		}
	}
	return true
}
