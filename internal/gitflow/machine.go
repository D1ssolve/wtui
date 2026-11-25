package gitflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/config"
	"gopkg.in/yaml.v3"
)

type StateKind string

const (
	StateRun   StateKind = "run"
	StateWait  StateKind = "wait"
	StateFinal StateKind = "final"
)

const (
	WaitManual       = "manual"
	WaitReviewMerged = "review_merged"
	WaitCIGreen      = "ci_green"

	OutcomeReleased = "released"
	OutcomeRejected = "rejected"

	EventActionMergeReviews = "merge_reviews"

	// ReleaseBranchRef names the per-service release branch in release steps.
	ReleaseBranchRef = "release"

	StepHook = "hook"
)

// Machine is a release lifecycle: run states execute actions, wait states park the
// release until a condition holds or the user fires an event, final states end it.
type Machine struct {
	Initial string
	States  []State
}

type State struct {
	ID      string
	Label   string
	Kind    StateKind
	Run     []Step
	Next    string
	OnError string
	Wait    string
	WaitOn  string
	Events  []Event
	Outcome string
}

type Event struct {
	Name   string
	Key    string
	Label  string
	To     string
	Action string
	When   string
}

func (m *Machine) State(id string) (*State, bool) {
	if m == nil {
		return nil, false
	}
	for i := range m.States {
		if m.States[i].ID == id {
			return &m.States[i], true
		}
	}
	return nil, false
}

func (m *Machine) Steps() []Step {
	var steps []Step
	if m == nil {
		return steps
	}
	for _, s := range m.States {
		steps = append(steps, s.Run...)
	}
	return steps
}

// Legacy wait-state ids equal the release statuses older versions persisted, so a
// manifest without a machine state continues in the state named by its status.
const (
	StatePreparing           = "preparing"
	StatePrepared            = "prepared"
	StatePromoting           = "promoting"
	StateAwaitingMasterMerge = "awaiting_master_merge"
	StateMasterMerged        = "master_merged"
	StateFinishing           = "finishing"
	StateReleased            = "released"
)

// ReleaseMachineFromSteps compiles a `release.steps` shorthand into its linear machine.
func ReleaseMachineFromSteps(steps []Step, production string) *Machine {
	return linearMachine(steps, production)
}

// DefaultReleaseMachine is the git-flow release lifecycle, used when a flow has no release configured.
func DefaultReleaseMachine(production, integration string) *Machine {
	return linearMachine([]Step{
		{Kind: StepCutBranch, From: integration},
		{Kind: StepGate, Label: "regression"},
		{Kind: StepReview, From: ReleaseBranchRef, Into: []string{production}, Merge: MergeStrategyMerge},
		{Kind: StepBackMerge, From: ReleaseBranchRef, Into: []string{integration}, When: "diverged"},
		{Kind: StepTag, On: "accepted_merge"},
	}, production)
}

func linearMachine(steps []Step, production string) *Machine {
	steps = slices.Clone(steps)
	gate := "approval"
	var prepare, finish []Step
	review := (*Step)(nil)
	for i := range steps {
		if steps[i].Kind == StepTag {
			steps[i].On = map[string]string{"accepted_merge": production, "release_branch": ReleaseBranchRef}[steps[i].On]
		}
		switch s := steps[i]; s.Kind {
		case StepIntegrateTasks, StepCutBranch:
			prepare = append(prepare, s)
		case StepGate:
			gate = s.Label
		case StepReview:
			review = &steps[i]
		case StepBackMerge, StepTag, StepPublish:
			finish = append(finish, s)
		}
	}
	advance := func(to, label string) []Event {
		return []Event{{Name: "advance", Key: "F", To: to, Label: label}}
	}
	finishLabel := "tag"
	for _, s := range finish {
		if s.Kind == StepBackMerge && len(s.Into) > 0 {
			finishLabel = "sync " + s.Into[0] + " and tag"
		}
	}
	m := &Machine{Initial: StatePreparing}
	m.States = append(m.States,
		State{ID: StatePreparing, Kind: StateRun, Label: "prepare", Run: prepare, Next: StatePrepared},
	)
	if review == nil {
		m.States = append(m.States, State{ID: StatePrepared, Kind: StateWait, Wait: WaitManual, Label: gate, Events: advance(StateFinishing, "tag the release branch")})
	} else {
		m.States = append(m.States,
			State{ID: StatePrepared, Kind: StateWait, Wait: WaitManual, Label: gate, Events: advance(StatePromoting, "create "+production+" MRs")},
			State{ID: StatePromoting, Kind: StateRun, Label: production + " MR", Run: []Step{*review}, Next: StateAwaitingMasterMerge},
			State{ID: StateAwaitingMasterMerge, Kind: StateWait, Wait: WaitReviewMerged, Label: "merge",
				Events: []Event{{Name: "merge", Key: "M", Label: "reconcile", Action: EventActionMergeReviews}}, Next: StateMasterMerged},
			State{ID: StateMasterMerged, Kind: StateWait, Wait: WaitManual, Label: "finish", Events: advance(StateFinishing, finishLabel)},
		)
	}
	m.States = append(m.States,
		State{ID: StateFinishing, Kind: StateRun, Label: "tag", Run: finish, Next: StateReleased},
		State{ID: StateReleased, Kind: StateFinal, Label: "released", Outcome: OutcomeReleased},
	)
	return m
}

// reservedStateNames are transient release statuses; a state with such a name would be
// indistinguishable from wtui working inside another state.
var reservedStateNames = []string{"draft", "validating", "merging", "branching", "pushing", "awaiting_task_merge", "integrating_tasks", "task_merge_blocked", "task_merge_partial", "syncing_develop", "tagging", "failed"}

// reservedReleaseKeys are bound by the Releases panel and the global keymap.
var reservedReleaseKeys = []string{"W", "E", "r", "j", "k", "g", "G", "h", "l", "N", "d", "x", "D", "P", "O", "R", "q", "?", ":", ",", ".", "/", "f", "L", "0", "1", "2", "3", "ctrl+r", "ctrl+p", "ctrl+c", "enter", "esc", "tab"}

// reservedTaskKeys are bound by the Tasks and Services panels and the global keymap;
// C and M stay available for the close and merge_reviews events that drive those screens.
var reservedTaskKeys = []string{"W", "E", "r", "j", "k", "g", "G", "h", "l", "N", "q", "?", ":", ",", ";", ".", "/", "f", "a", "c", "D", "P", "d", "H", "i", "I", "m", "O", "R", "S", "T", "v", "V", "X", "L", "0", "1", "2", "3", "ctrl+r", "ctrl+p", "ctrl+c", "enter", "esc", "tab"}

const (
	// TaskBranchRef names each service's task branch in task states.
	TaskBranchRef = "task"

	EventActionClose = "close"
)

// machineScope is what differs between a release machine and a task type's machine.
type machineScope struct {
	base      string
	ctx       stepContext
	branchRef string
	keys      []string
	reviews   bool
}

func (c *compiler) releaseScope() machineScope {
	return machineScope{base: "workflow.release.machine", ctx: contextRelease, branchRef: ReleaseBranchRef, keys: reservedReleaseKeys, reviews: true}
}

func validKey(key string) bool {
	if strings.HasPrefix(key, "ctrl+") {
		return len(key) == len("ctrl+")+1
	}
	return len([]rune(key)) == 1 && key != " "
}

func (c *compiler) machine(scope machineScope, cfg *config.MachineConfig) (*Machine, error) {
	base := scope.base
	if len(cfg.States) == 0 {
		return nil, fmt.Errorf("%s.states: at least one state is required", base)
	}
	m := &Machine{Initial: cfg.Initial}
	if m.Initial == "" {
		m.Initial = cfg.States[0].Name
	}
	ids := map[string]bool{}
	for _, sc := range cfg.States {
		if ids[sc.Name] {
			return nil, fmt.Errorf("%s.states.%s: duplicate state", base, sc.Name)
		}
		if slices.Contains(reservedStateNames, sc.Name) {
			return nil, fmt.Errorf("%s.states.%s: name is reserved for a release status wtui sets while it works", base, sc.Name)
		}
		ids[sc.Name] = true
	}
	if !ids[m.Initial] {
		return nil, fmt.Errorf("%s.initial: unknown state %q", base, m.Initial)
	}
	for _, sc := range cfg.States {
		st, err := c.machineState(scope, base+".states."+sc.Name, sc, ids)
		if err != nil {
			return nil, err
		}
		m.States = append(m.States, st)
	}
	if err := validateMachine(base, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *compiler) machineState(scope machineScope, path string, sc config.StateConfig, ids map[string]bool) (State, error) {
	st := State{ID: sc.Name, Label: sc.Label, Next: sc.Next, OnError: sc.OnError}
	kinds := 0
	if sc.Run != nil {
		kinds++
		st.Kind = StateRun
	}
	if sc.Wait != nil {
		kinds++
		st.Kind = StateWait
	}
	if sc.Final != "" {
		kinds++
		st.Kind = StateFinal
	}
	if kinds != 1 {
		return State{}, fmt.Errorf("%s: a state needs exactly one of run, wait or final", path)
	}
	if st.Label == "" {
		st.Label = sc.Name
	}
	for _, ref := range []struct{ key, to string }{{"next", st.Next}, {"on_error", st.OnError}} {
		if ref.to != "" && !ids[ref.to] {
			return State{}, fmt.Errorf("%s.%s: unknown state %q", path, ref.key, ref.to)
		}
	}

	switch st.Kind {
	case StateFinal:
		if sc.Final != OutcomeReleased && sc.Final != OutcomeRejected {
			return State{}, fmt.Errorf("%s.final: expected released or rejected, got %q", path, sc.Final)
		}
		st.Outcome = sc.Final
		if st.Next != "" || st.OnError != "" || len(sc.On) > 0 {
			return State{}, fmt.Errorf("%s: a final state has no next, on_error or on", path)
		}
		return st, nil
	case StateRun:
		if len(sc.Run) == 0 {
			return State{}, fmt.Errorf("%s.run: at least one action is required", path)
		}
		if st.Next == "" {
			return State{}, fmt.Errorf("%s.next is required after run", path)
		}
		if len(sc.On) > 0 {
			return State{}, fmt.Errorf("%s.on: events belong to wait states", path)
		}
		for i, raw := range sc.Run {
			stepPath := fmt.Sprintf("%s.run[%d]", path, i)
			step, err := c.step(stepPath, scope.ctx, raw)
			if err != nil {
				return State{}, err
			}
			if err := c.machineStep(scope, stepPath+"."+step.Kind, &step); err != nil {
				return State{}, err
			}
			st.Run = append(st.Run, step)
		}
		return st, nil
	}

	if st.OnError != "" {
		return State{}, fmt.Errorf("%s.on_error: only run states can fail", path)
	}
	wait, on, err := parseWait(sc.Wait)
	if err != nil {
		return State{}, fmt.Errorf("%s.wait: %w", path, err)
	}
	st.Wait = wait
	if wait == WaitReviewMerged && !scope.reviews {
		return State{}, fmt.Errorf("%s.wait: review_merged needs a task type that closes with review", path)
	}
	if wait == WaitCIGreen && on == "" {
		on = scope.branchRef
	}
	if on != "" {
		if st.WaitOn, err = c.scopeBranch(scope, path+".wait."+wait, on); err != nil {
			return State{}, err
		}
	}
	if wait == WaitManual && st.Next != "" {
		return State{}, fmt.Errorf("%s.next: a manual wait leaves only through its events", path)
	}
	if wait != WaitManual && st.Next == "" {
		return State{}, fmt.Errorf("%s.next is required: where to go once %s holds", path, wait)
	}
	keys := map[string]string{}
	for _, ec := range sc.On {
		evPath := path + ".on." + ec.Name
		ev := Event{Name: ec.Name, Key: ec.Key, Label: ec.Label, To: ec.To, Action: ec.Action, When: ec.When}
		if ev.Label == "" {
			ev.Label = ev.Name
		}
		if !validKey(ev.Key) {
			return State{}, fmt.Errorf("%s.key: expected one character or ctrl+<char>, got %q", evPath, ev.Key)
		}
		if slices.Contains(scope.keys, ev.Key) {
			return State{}, fmt.Errorf("%s.key: %q is already bound by wtui", evPath, ev.Key)
		}
		if other, taken := keys[ev.Key]; taken {
			return State{}, fmt.Errorf("%s.key: %q is already used by event %s", evPath, ev.Key, other)
		}
		keys[ev.Key] = ev.Name
		switch ev.Action {
		case "":
			if ev.To == "" {
				return State{}, fmt.Errorf("%s: an event needs to or action", evPath)
			}
			if ev.Key == "M" {
				return State{}, fmt.Errorf("%s.key: M is reserved for merge_reviews", evPath)
			}
			if ev.Key == "C" && scope.ctx == contextTaskState {
				return State{}, fmt.Errorf("%s.key: C is reserved for the close event", evPath)
			}
		case EventActionMergeReviews:
			if wait != WaitReviewMerged {
				return State{}, fmt.Errorf("%s.action: merge_reviews belongs to a review_merged wait", evPath)
			}
			if ev.Key != "M" {
				return State{}, fmt.Errorf("%s.key: merge_reviews uses M, which opens the merge review screen", evPath)
			}
			if ev.To != "" {
				return State{}, fmt.Errorf("%s.to: merge_reviews leaves through the wait's next once every review is merged", evPath)
			}
		case EventActionClose:
			if scope.ctx != contextTaskState {
				return State{}, fmt.Errorf("%s.action: close belongs to task type machines", evPath)
			}
			if wait != WaitManual || ev.Key != "C" || ev.To == "" {
				return State{}, fmt.Errorf("%s: close is a C event with a to, in a manual wait; C opens the close confirmation", evPath)
			}
		default:
			return State{}, fmt.Errorf("%s.action: unknown action %q, expected merge_reviews or close", evPath, ev.Action)
		}
		if ev.To != "" && !ids[ev.To] {
			return State{}, fmt.Errorf("%s.to: unknown state %q", evPath, ev.To)
		}
		switch ev.When {
		case "", WaitCIGreen:
		default:
			return State{}, fmt.Errorf("%s.when: expected ci_green", evPath)
		}
		st.Events = append(st.Events, ev)
	}
	if wait == WaitManual && len(st.Events) == 0 {
		return State{}, fmt.Errorf("%s.on: a manual wait needs at least one event", path)
	}
	return st, nil
}

func parseWait(node *yaml.Node) (string, string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		switch node.Value {
		case WaitManual, WaitReviewMerged:
			return node.Value, "", nil
		case WaitCIGreen:
			return WaitCIGreen, "", nil
		}
	case yaml.MappingNode:
		if len(node.Content) == 2 && node.Content[0].Value == WaitCIGreen && node.Content[1].Kind == yaml.ScalarNode {
			return WaitCIGreen, node.Content[1].Value, nil
		}
	}
	return "", "", fmt.Errorf("expected manual, review_merged, ci_green or {ci_green: <branch>}")
}

// releaseBranch resolves branch references in release states; "release" stays the
// per-service release branch.
func (c *compiler) releaseBranch(path, value string) (string, error) {
	return c.scopeBranch(c.releaseScope(), path, value)
}

// scopeBranch keeps the per-service branch reference of the scope ("release" or "task").
func (c *compiler) scopeBranch(scope machineScope, path, value string) (string, error) {
	if value == scope.branchRef {
		return value, nil
	}
	return c.branch(path, value)
}

func (c *compiler) machineStep(scope machineScope, path string, step *Step) error {
	var err error
	switch step.Kind {
	case StepGate:
		return fmt.Errorf("%s: gate is a steps shorthand; use a manual wait state in a machine", path)
	case StepCutBranch:
		step.From, err = c.branch(path+".from", step.From)
		if err == nil && step.From != c.flow.IntegrationBranch {
			return fmt.Errorf("%s.from: release branches are cut from the integration branch (%s)", path, c.flow.IntegrationBranch)
		}
	case StepReview:
		if step.From, err = c.releaseBranch(path+".from", step.From); err != nil {
			return err
		}
		if len(step.Into) != 1 {
			return fmt.Errorf("%s.into: a release review has exactly one target", path)
		}
		if step.Into[0] == step.From {
			return fmt.Errorf("%s: from and into are the same branch", path)
		}
	case StepBackMerge:
		if step.From, err = c.releaseBranch(path+".from", step.From); err != nil {
			return err
		}
		if len(step.Into) != 1 {
			return fmt.Errorf("%s.into: back_merge has exactly one target", path)
		}
		if step.When == "" {
			step.When = "diverged"
		}
		if step.When != "diverged" {
			return fmt.Errorf("%s.when: only diverged is supported", path)
		}
	case StepTag:
		switch step.On {
		case "release_branch", ReleaseBranchRef:
			step.On = ReleaseBranchRef
		default:
			step.On, err = c.branch(path+".on", step.On)
		}
	case StepPipeline:
		if step.On == "" {
			step.On = scope.branchRef
		}
		step.On, err = c.scopeBranch(scope, path+".on", step.On)
	case StepHook:
		if strings.TrimSpace(step.Command) == "" {
			return fmt.Errorf("%s.command is required", path)
		}
		if step.Name == "" {
			step.Name = "hook"
		}
		if step.Timeout == "" {
			step.Timeout = "10m"
		}
		if _, perr := time.ParseDuration(step.Timeout); perr != nil {
			return fmt.Errorf("%s.timeout: %v", path, perr)
		}
	case StepPublish:
		return validatePublish(path, step)
	}
	return err
}

type machineFact string

func factCut() machineFact            { return "cut" }
func factTagged() machineFact         { return "tagged" }
func factOpened(b string) machineFact { return machineFact("opened:" + b) }
func factMerged(b string) machineFact { return machineFact("merged:" + b) }
func (f machineFact) opened() (string, bool) {
	return strings.CutPrefix(string(f), "opened:")
}

func validateMachine(base string, m *Machine) error {
	byID := map[string]*State{}
	for i := range m.States {
		byID[m.States[i].ID] = &m.States[i]
	}
	succ := func(s *State) []string {
		var out []string
		for _, to := range []string{s.Next, s.OnError} {
			if to != "" {
				out = append(out, to)
			}
		}
		for _, ev := range s.Events {
			if ev.To != "" {
				out = append(out, ev.To)
			}
		}
		return out
	}

	reached := map[string]bool{m.Initial: true}
	queue := []string{m.Initial}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, to := range succ(byID[id]) {
			if !reached[to] {
				reached[to] = true
				queue = append(queue, to)
			}
		}
	}
	hasFinal := false
	for _, s := range m.States {
		if !reached[s.ID] {
			return fmt.Errorf("%s.states.%s: unreachable from %s", base, s.ID, m.Initial)
		}
		hasFinal = hasFinal || s.Kind == StateFinal
	}
	if !hasFinal {
		return fmt.Errorf("%s.states: at least one final state is required", base)
	}

	// Run states and condition waits advance on their own; a cycle of only those would
	// spin without ever waiting for the user.
	automatic := func(s *State) bool {
		return s.Kind == StateRun || s.Kind == StateWait && s.Wait != WaitManual
	}
	color := map[string]int{}
	var visit func(id string) error
	visit = func(id string) error {
		color[id] = 1
		s := byID[id]
		for _, to := range []string{s.Next, s.OnError} {
			if to == "" || !automatic(byID[to]) {
				continue
			}
			switch color[to] {
			case 1:
				return fmt.Errorf("%s.states.%s: loops back to %s without a manual wait state", base, id, to)
			case 0:
				if err := visit(to); err != nil {
					return err
				}
			}
		}
		color[id] = 2
		return nil
	}
	for i := range m.States {
		if s := &m.States[i]; automatic(s) && color[s.ID] == 0 {
			if err := visit(s.ID); err != nil {
				return err
			}
		}
	}

	return validateMachineDataFlow(base, m, byID, succ)
}

// validateMachineDataFlow proves every action's inputs exist on every path that reaches
// it: a release branch before it is used, and a merged review before tagging its target.
func validateMachineDataFlow(base string, m *Machine, byID map[string]*State, succ func(*State) []string) error {
	transfer := func(s *State, in map[machineFact]bool) map[machineFact]bool {
		out := map[machineFact]bool{}
		for f := range in {
			out[f] = true
		}
		for _, step := range s.Run {
			switch step.Kind {
			case StepCutBranch:
				out[factCut()] = true
			case StepReview:
				out[factOpened(step.Into[0])] = true
			case StepTag:
				out[factTagged()] = true
			}
		}
		return out
	}
	// Reviews count as merged only on the edge a review_merged wait takes once they are.
	edgeOut := func(s *State, out map[machineFact]bool, to string) map[machineFact]bool {
		if slices.ContainsFunc(s.Events, func(ev Event) bool { return ev.Action == EventActionClose && ev.To == to }) {
			closed := map[machineFact]bool{factOpened(TaskBranchRef): true}
			for f := range out {
				closed[f] = true
			}
			return closed
		}
		if s.Kind != StateWait || s.Wait != WaitReviewMerged || to != s.Next {
			return out
		}
		merged := map[machineFact]bool{}
		for f := range out {
			merged[f] = true
			if b, ok := f.opened(); ok {
				merged[factMerged(b)] = true
			}
		}
		return merged
	}
	universe := map[machineFact]bool{factCut(): true, factTagged(): true, factOpened(TaskBranchRef): true, factMerged(TaskBranchRef): true}
	for _, s := range m.States {
		for _, step := range s.Run {
			if step.Kind == StepReview {
				universe[factOpened(step.Into[0])] = true
				universe[factMerged(step.Into[0])] = true
			}
		}
	}
	in := map[string]map[machineFact]bool{}
	for _, s := range m.States {
		in[s.ID] = universe
	}
	in[m.Initial] = map[machineFact]bool{}
	for changed := true; changed; {
		changed = false
		for i := range m.States {
			s := &m.States[i]
			out := transfer(s, in[s.ID])
			for _, to := range succ(s) {
				out := edgeOut(s, out, to)
				if s.Kind == StateWait && s.Wait == WaitReviewMerged && to == s.Next && slices.ContainsFunc(s.Events, func(ev Event) bool { return ev.To == to }) {
					out = transfer(s, in[s.ID])
				}
				meet := map[machineFact]bool{}
				for f := range in[to] {
					if out[f] {
						meet[f] = true
					}
				}
				if to == m.Initial {
					continue
				}
				if len(meet) != len(in[to]) {
					in[to] = meet
					changed = true
				}
			}
		}
	}

	for _, s := range m.States {
		have := map[machineFact]bool{}
		for f := range in[s.ID] {
			have[f] = true
		}
		path := base + ".states." + s.ID
		for i, step := range s.Run {
			stepPath := fmt.Sprintf("%s.run[%d].%s", path, i, step.Kind)
			needsCut := (step.Kind == StepReview || step.Kind == StepBackMerge) && step.From == ReleaseBranchRef ||
				(step.Kind == StepTag || step.Kind == StepPipeline) && step.On == ReleaseBranchRef
			if needsCut && !have[factCut()] {
				return fmt.Errorf("%s: uses the release branch before cut_branch on some path", stepPath)
			}
			if step.Kind == StepTag && step.On != ReleaseBranchRef && !have[factMerged(step.On)] {
				return fmt.Errorf("%s.on: no merged review into %s precedes this tag on every path", stepPath, step.On)
			}
			if step.Kind == StepPublish && !have[factTagged()] {
				return fmt.Errorf("%s: no tag step precedes this publish on every path; publish releases an existing tag", stepPath)
			}
			if step.Kind == StepIntegrateTasks && (s.ID != m.Initial || i != 0 || len(s.Run) != 2 || s.Run[1].Kind != StepCutBranch) {
				return fmt.Errorf("%s: integrate_tasks must be followed only by cut_branch in the initial state", stepPath)
			}
			if step.Kind == StepCutBranch && s.ID != m.Initial {
				return fmt.Errorf("%s: cut_branch belongs to the initial state, which prepares the release", stepPath)
			}
			switch step.Kind {
			case StepCutBranch:
				have[factCut()] = true
			case StepReview:
				have[factOpened(step.Into[0])] = true
			case StepTag:
				have[factTagged()] = true
			}
		}
		if s.Kind == StateWait && s.Wait == WaitReviewMerged {
			opened := false
			for f := range have {
				_, ok := f.opened()
				opened = opened || ok
			}
			if !opened {
				return fmt.Errorf("%s.wait: no review is opened before this review_merged wait on every path", path)
			}
		}
		if s.Kind == StateWait && s.WaitOn == ReleaseBranchRef && !have[factCut()] {
			return fmt.Errorf("%s.wait: ci_green on the release branch before cut_branch on some path", path)
		}
	}
	return nil
}

// Fingerprint identifies the behaviour of the machine; labels do not change it.
func (m *Machine) Fingerprint() string {
	sum := sha256.Sum256([]byte(m.canonical()))
	return hex.EncodeToString(sum[:])
}

func (m *Machine) canonical() string {
	var b strings.Builder
	fmt.Fprintf(&b, "initial=%s", m.Initial)
	for _, s := range m.States {
		fmt.Fprintf(&b, "|%s:%s:%s:%s:%s:%s:%s", s.ID, s.Kind, s.Next, s.OnError, s.Wait, s.WaitOn, s.Outcome)
		for _, st := range s.Run {
			fmt.Fprintf(&b, ";%s:%s:%s:%s:%s:%s:%s:%s", st.Kind, st.From, strings.Join(st.Into, ","), st.Merge, st.On, st.When, st.Command, st.Timeout)
			if st.Kind == StepPublish { // only here, so fingerprints of flows without publish stay unchanged
				fmt.Fprintf(&b, ":%q:%s", st.Title, st.Notes)
			}
		}
		for _, ev := range s.Events {
			fmt.Fprintf(&b, ";ev:%s:%s:%s:%s:%s", ev.Name, ev.Key, ev.To, ev.Action, ev.When)
		}
	}
	return b.String()
}
