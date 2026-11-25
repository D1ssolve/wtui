package gitflow

import (
	"fmt"
	"html"
	"io"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/D1ssolve/wtui/internal/config"
)

// Describe is the plain-text summary printed by `wtui flow`.
func (flow *ResolvedGitFlow) Describe() string {
	var b strings.Builder
	preset := flow.Preset
	if preset == "" {
		preset = "custom"
	}
	fmt.Fprintf(&b, "workflow: %s\nbranches: production=%s integration=%s\ntask types:\n", preset, flow.ProductionBranch, flow.IntegrationBranch)
	names := make([]string, 0, len(flow.BranchTypes))
	for bt := range flow.BranchTypes {
		names = append(names, string(bt))
	}
	slices.Sort(names)
	for _, name := range names {
		rule := flow.BranchTypes[BranchType(name)]
		prefixes := strings.Join(rule.Prefixes, ", ")
		if prefixes == "" {
			prefixes = "(any other branch)"
		}
		marker := ""
		if BranchType(name) == flow.DefaultBranchType {
			marker = " (default)"
		}
		fmt.Fprintf(&b, "  %s%s: prefixes %s, from %s\n    close: %s\n", name, marker, prefixes, rule.BaseBranch, DescribeSteps(rule.Close, ", "))
		if rule.Machine != nil {
			fmt.Fprintf(&b, "    machine, starts in %s (C closes, M merges only where an event offers it):\n", rule.Machine.Initial)
			describeMachine(&b, rule.Machine, "      ")
		}
	}
	if flow.Release == nil {
		b.WriteString("release: not configured\n")
		return b.String()
	}
	m := flow.Release.Machine
	fmt.Fprintf(&b, "release: branch %s<version>, starts in %s\n", flow.Release.BranchPrefix, m.Initial)
	describeMachine(&b, m, "  ")
	return b.String()
}

func DescribeSteps(steps []Step, sep string) string {
	if len(steps) == 0 {
		return "nothing"
	}
	parts := make([]string, len(steps))
	for i, s := range steps {
		text := s.Kind
		if s.From != "" {
			text += " from " + s.From
		}
		if len(s.Into) > 0 {
			text += " into " + strings.Join(s.Into, ", ")
		}
		if s.Merge != "" {
			text += " (" + string(s.Merge) + ")"
		}
		if s.On != "" {
			text += " on " + s.On
		}
		if s.Label != "" {
			text += " \"" + s.Label + "\""
		}
		if s.ViaActiveRelease {
			text += " via active release"
		}
		if s.Command != "" {
			text += fmt.Sprintf(" %s: %q", s.Name, s.Command)
		}
		parts[i] = text
	}
	return strings.Join(parts, sep)
}

var mermaidUnsafe = regexp.MustCompile(`[^A-Za-z0-9_]`)

func mermaidID(parts ...string) string {
	return mermaidUnsafe.ReplaceAllString(strings.Join(parts, "_"), "_")
}

func mermaidLabel(s string) string {
	return strings.NewReplacer(`"`, "'", "<", "‹", ">", "›").Replace(s)
}

func describeMachine(b *strings.Builder, m *Machine, indent string) {
	for _, st := range m.States {
		switch st.Kind {
		case StateRun:
			fmt.Fprintf(b, indent+"%s: run %s -> %s", st.ID, DescribeSteps(st.Run, ", "), st.Next)
			if st.OnError != "" {
				fmt.Fprintf(b, " (on error -> %s)", st.OnError)
			}
		case StateWait:
			fmt.Fprintf(b, indent+"%s: wait %s", st.ID, st.Wait)
			if st.WaitOn != "" {
				fmt.Fprintf(b, " on %s", st.WaitOn)
			}
			if st.Label != st.ID {
				fmt.Fprintf(b, " %q", st.Label)
			}
			if st.Next != "" {
				fmt.Fprintf(b, " -> %s", st.Next)
			}
			for _, ev := range st.Events {
				target := ev.To
				if ev.Action != "" {
					target = strings.TrimSuffix(ev.Action+" then "+ev.To, " then ")
				}
				fmt.Fprintf(b, " [%s %s -> %s]", ev.Key, ev.Name, target)
			}
		case StateFinal:
			fmt.Fprintf(b, indent+"%s: final %s", st.ID, st.Outcome)
		}
		b.WriteString("\n")
	}
}

// Mermaid renders the whole workflow as a flowchart: one lane per task type from branch to
// its close steps, and the release lifecycle with its waits, events and keys.
func (flow *ResolvedGitFlow) Mermaid() string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	names := make([]string, 0, len(flow.BranchTypes))
	for bt := range flow.BranchTypes {
		if bt != BranchTypeRelease {
			names = append(names, string(bt))
		}
	}
	slices.Sort(names)
	for _, name := range names {
		rule := flow.BranchTypes[BranchType(name)]
		branch := strings.Join(rule.Prefixes, " | ")
		if branch == "" {
			branch = "any other branch"
		}
		fmt.Fprintf(&b, "    subgraph %s[\"%s\"]\n", mermaidID("task", name), mermaidLabel(name))
		if rule.Machine != nil {
			prefix := mermaidID("task", name, "m")
			fmt.Fprintf(&b, "        %s([\"%s from %s\"]) --> %s\n", mermaidID("task", name, "start"), mermaidLabel(branch), mermaidLabel(rule.BaseBranch), mermaidID(prefix, rule.Machine.Initial))
			mermaidMachine(&b, rule.Machine, prefix)
			b.WriteString("    end\n")
			continue
		}
		prev := mermaidID("task", name, "start")
		fmt.Fprintf(&b, "        %s([\"%s from %s\"])\n", prev, mermaidLabel(branch), mermaidLabel(rule.BaseBranch))
		for i, step := range rule.Close {
			id := mermaidID("task", name, fmt.Sprint(i))
			fmt.Fprintf(&b, "        %s[\"%s\"]\n        %s -->|%s| %s\n", id, mermaidLabel(DescribeSteps([]Step{step}, "")), prev, map[bool]string{true: "C", false: "then"}[i == 0], id)
			prev = id
		}
		done := mermaidID("task", name, "done")
		fmt.Fprintf(&b, "        %s((done))\n        %s --> %s\n    end\n", done, prev, done)
	}
	if flow.Release == nil {
		return b.String()
	}
	m := flow.Release.Machine
	fmt.Fprintf(&b, "    subgraph release[\"release %s‹version›\"]\n", mermaidLabel(flow.Release.BranchPrefix))
	fmt.Fprintf(&b, "        release_entry([\"N: new release\"]) --> %s\n", mermaidID("rel", m.Initial))
	mermaidMachine(&b, m, "rel")
	b.WriteString("    end\n")
	return b.String()
}

func mermaidMachine(b *strings.Builder, m *Machine, prefix string) {
	for _, st := range m.States {
		id := mermaidID(prefix, st.ID)
		switch st.Kind {
		case StateRun:
			fmt.Fprintf(b, "        %s[\"%s: %s\"]\n", id, mermaidLabel(st.ID), mermaidLabel(DescribeSteps(st.Run, ", ")))
		case StateWait:
			fmt.Fprintf(b, "        %s{{\"%s: wait %s\"}}\n", id, mermaidLabel(st.Label), mermaidLabel(strings.ReplaceAll(st.Wait, "_", " ")))
		case StateFinal:
			fmt.Fprintf(b, "        %s((%s))\n", id, mermaidLabel(st.Outcome))
		}
	}
	for _, st := range m.States {
		id := mermaidID(prefix, st.ID)
		if st.Next != "" {
			label := ""
			if st.Kind == StateWait {
				label = "|" + strings.ReplaceAll(st.Wait, "_", " ") + "|"
			}
			fmt.Fprintf(b, "        %s -->%s %s\n", id, label, mermaidID(prefix, st.Next))
		}
		if st.OnError != "" {
			fmt.Fprintf(b, "        %s -.->|error| %s\n", id, mermaidID(prefix, st.OnError))
		}
		for _, ev := range st.Events {
			if ev.To != "" {
				fmt.Fprintf(b, "        %s -->|\"%s %s\"| %s\n", id, ev.Key, mermaidLabel(ev.Label), mermaidID(prefix, ev.To))
			}
		}
	}
}

// HTML is a standalone page rendering Mermaid(); the browser loads mermaid.js from a CDN.
func (flow *ResolvedGitFlow) HTML() string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>wtui workflow</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;background:#fafafa;color:#222}pre.text{background:#fff;border:1px solid #ddd;padding:1rem;overflow:auto}</style>
</head>
<body>
<h1>wtui workflow</h1>
<pre class="mermaid">
` + html.EscapeString(flow.Mermaid()) + `</pre>
<h2>Details</h2>
<pre class="text">` + html.EscapeString(flow.Describe()) + `</pre>
<script type="module">
import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs";
mermaid.initialize({startOnLoad: true, securityLevel: "strict"});
</script>
</body>
</html>
`
}

// FormatReference documents the workflow format from the compiler's own tables and the
// embedded presets, so a model drafting configuration sees exactly what wtui accepts.
func FormatReference() string {
	var b strings.Builder
	b.WriteString("Top level: workflow: {extends, branches: {production, integration}, default_type, task_types: {<name>: {prefixes, from, shared, requires_clean, close: [steps]}}, release: {branch: <prefix>{{.Version}}, steps: [steps] | machine}}\n")
	b.WriteString("Branch references: production, integration, a literal branch name; in release states also release (the release branch).\n")
	b.WriteString("A step is a single-key mapping {kind: {params}}. Kinds and params by context:\n")
	kinds := make([]string, 0, len(stepSpecs))
	for k := range stepSpecs {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	for _, k := range kinds {
		for _, ctx := range []stepContext{contextTask, contextRelease} {
			params, ok := stepSpecs[k][ctx]
			if !ok {
				continue
			}
			where := map[stepContext]string{contextTask: "task close", contextRelease: "release"}[ctx]
			fmt.Fprintf(&b, "  %s (%s): %s\n", k, where, strings.Join(params, ", "))
		}
	}
	b.WriteString("Task close order: [merge|review] [tag] [cherry_pick] [pipeline]. Merge strategies: merge_commit, squash, rebase, ff_only.\n")
	b.WriteString("release.steps shorthand: [integrate_tasks] cut_branch [gate] then review [back_merge] tag{on: accepted_merge} or tag{on: release_branch}.\n")
	b.WriteString("release.machine: {initial, states: {<id>: exactly one of run: [steps] + next (+ on_error) | wait: manual|review_merged|ci_green|{ci_green: <branch>} + next (not for manual) + on: {<event>: {key, to|action: merge_reviews, when: ci_green, label}} | final: released|rejected}}.\n")
	b.WriteString("Machine rules: every state reachable, a final state exists, loops pass a manual wait, cut_branch and integrate_tasks only in the initial state, tag on a branch needs a merged review into it on every path, merge_reviews uses key M and has no to; a task machine close event uses key C with to.\n")
	fmt.Fprintf(&b, "Keys release events must not use: %s\nKeys task events must not use: %s\n", strings.Join(reservedReleaseKeys, " "), strings.Join(reservedTaskKeys, " "))
	for _, name := range PresetNames() {
		data, _ := presetFS.ReadFile("presets/" + name + ".yaml")
		fmt.Fprintf(&b, "\nPreset %s (use with extends: %s):\n%s", name, name, data)
	}
	return b.String()
}

// ParseWorkflowYAML compiles a document that holds only a `workflow:` block.
func ParseWorkflowYAML(text string) (*ResolvedGitFlow, error) {
	var doc struct {
		Workflow *config.WorkflowConfig `yaml:"workflow"`
	}
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	if decoder.Decode(new(yaml.Node)) != io.EOF {
		return nil, fmt.Errorf("expected a single YAML document")
	}
	if doc.Workflow == nil {
		return nil, fmt.Errorf("workflow: block is missing")
	}
	return compileWorkflow(doc.Workflow)
}
