package modal

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

type builderField struct {
	label, value string
	set          func(string)
	act          func()
	choices      []string
}

// builderFields describes the current draft; rebuild it after applying an edit.
func builderFields(d *gitflow.Draft) []builderField {
	fields := []builderField{
		builderText("Production branch", &d.Production),
		builderText("Integration branch", &d.Integration),
	}
	if len(d.Types) > 0 {
		field := builderText("Default task type", &d.DefaultType)
		for _, task := range d.Types {
			field.choices = append(field.choices, task.Name)
		}
		fields = append(fields, field)
	}
	for i := range d.Types {
		task := &d.Types[i]
		label := fmt.Sprintf("Task %d", i+1)
		fields = append(fields, builderField{label: label + " name", value: task.Name, set: func(name string) {
			if name == task.Name {
				return
			}
			old := task.Name
			task.Name = name
			retireBuilderType(d, old, name)
		}})
		fields = append(fields,
			builderList(label+" prefixes", &task.Prefixes),
			builderText(label+" from", &task.From),
			builderBool(label+" shared", &task.Shared),
		)
		fields = append(fields, builderMRFields(label, &task.MR)...)
		fields = append(fields, builderStepFields(label+" close step", &task.Close, false)...)
		fields = append(fields, builderField{label: fmt.Sprintf("Delete task %d", i+1), act: func() {
			old := task.Name
			d.Types = slices.Delete(d.Types, i, i+1)
			fallback := ""
			if len(d.Types) > 0 {
				fallback = d.Types[0].Name
			}
			retireBuilderType(d, old, fallback)
		}})
	}
	fields = append(fields, builderField{label: "Add task type", act: func() {
		name := "task"
		for i := 2; d.Type(name) != nil; i++ {
			name = fmt.Sprintf("task-%d", i)
		}
		d.Types = append(d.Types, gitflow.DraftType{Name: name, From: "integration", Prefixes: []string{name + "/"}})
		if len(d.Types) == 1 {
			d.DefaultType = name
		}
	}})
	if release := d.Release; release != nil {
		fields = append(fields, builderText("Release branch", &release.Branch))
		fields = append(fields, builderMRFields("Release", &release.MR)...)
		fields = append(fields, builderStepFields("Release step", &release.Steps, true)...)
	} else {
		fields = append(fields, builderField{label: "Add linear release", act: func() {
			d.Release = &gitflow.DraftRelease{Branch: "release/{{.Version}}", Steps: []gitflow.DraftStep{
				{Kind: gitflow.StepCutBranch, Params: map[string][]string{"from": {"integration"}}},
				{Kind: gitflow.StepTag, Params: map[string][]string{"on": {"release_branch"}}},
			}}
		}})
	}
	return fields
}

func retireBuilderType(d *gitflow.Draft, old, fallback string) {
	if d.Type(old) != nil {
		return
	}
	d.RemoveType(old) // Record a preset tombstone now that no draft row uses the name.
	if d.DefaultType == old {
		d.DefaultType = fallback
	}
}

func builderText(label string, value *string) builderField {
	return builderField{label: label, value: *value, set: func(s string) { *value = s }}
}

func builderBool(label string, value *bool) builderField {
	return builderField{
		label: label, value: strconv.FormatBool(*value), choices: []string{"false", "true"},
		set: func(s string) { *value = s == "true" },
	}
}

func builderList(label string, values *[]string) builderField {
	return builderField{label: label, value: strings.Join(*values, ", "), set: func(s string) {
		*values = splitBuilderList(s)
	}}
}

func splitBuilderList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	values := strings.Split(s, ",")
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
	}
	return values
}

func builderMRFields(label string, mr *config.MRConfig) []builderField {
	return []builderField{
		builderBool(label+" MR draft", &mr.Draft),
		builderList(label+" MR labels", &mr.Labels),
		builderList(label+" MR reviewers", &mr.Reviewers),
		builderList(label+" MR assignees", &mr.Assignees),
		builderBool(label+" MR remove source branch", &mr.RemoveSourceBranch),
	}
}

func builderStepFields(label string, steps *[]gitflow.DraftStep, release bool) []builderField {
	kinds := gitflow.StepKinds(release)
	actionLabel := strings.ToLower(label)
	var fields []builderField
	for i := range *steps {
		step := &(*steps)[i]
		row := fmt.Sprintf("%s %d", label, i+1)
		fields = append(fields, builderField{label: row + " kind", value: step.Kind, choices: kinds, set: func(kind string) {
			if step.Kind == kind {
				return
			}
			step.Kind = kind
			allowed := gitflow.StepParams(kind, release)
			for key := range step.Params {
				if !slices.Contains(allowed, key) {
					delete(step.Params, key)
				}
			}
		}})
		for _, key := range gitflow.StepParams(step.Kind, release) {
			fields = append(fields, builderField{label: row + " " + key, value: strings.Join(step.Params[key], ", "), set: func(s string) {
				if s == "" {
					delete(step.Params, key)
					return
				}
				if step.Params == nil {
					step.Params = make(map[string][]string)
				}
				if key == "into" {
					step.Params[key] = splitBuilderList(s)
				} else {
					step.Params[key] = []string{s}
				}
			}})
		}
		actionRow := fmt.Sprintf("%s %d", actionLabel, i+1)
		if i > 0 {
			fields = append(fields, builderField{label: "Move " + actionRow + " up", act: func() {
				(*steps)[i-1], (*steps)[i] = (*steps)[i], (*steps)[i-1]
			}})
		}
		if i+1 < len(*steps) {
			fields = append(fields, builderField{label: "Move " + actionRow + " down", act: func() {
				(*steps)[i], (*steps)[i+1] = (*steps)[i+1], (*steps)[i]
			}})
		}
		fields = append(fields, builderField{label: "Delete " + actionRow, act: func() {
			*steps = slices.Delete(*steps, i, i+1)
		}})
	}
	return append(fields, builderField{label: "Add " + actionLabel, act: func() {
		kind := gitflow.StepReview
		if release {
			kind = gitflow.StepCutBranch
		}
		*steps = append(*steps, gitflow.DraftStep{Kind: kind})
	}})
}
