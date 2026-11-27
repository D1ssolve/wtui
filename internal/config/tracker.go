package config

import (
	"fmt"
	"regexp"
	"strings"
)

type TrackerConfig struct {
	Plugin      string             `yaml:"plugin"`
	Queries     []TrackerQuery     `yaml:"queries"`
	BranchTypes map[string]string  `yaml:"branch_types"`
	Transitions TrackerTransitions `yaml:"transitions"`
	CommentMRs  bool               `yaml:"comment_mrs"`
}

type TrackerQuery struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

type TrackerTransitions struct {
	TaskCreated string `yaml:"task_created"`
	MROpened    string `yaml:"mr_opened"`
	MRMerged    string `yaml:"mr_merged"`
}

const defaultTrackerQuery = `assignee = currentUser() AND statusCategory = "To Do" ORDER BY updated DESC`

var trackerPluginPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func (c *Config) effectiveTracker() error {
	t := c.Tracker
	if t == nil {
		return nil
	}
	t.Plugin = strings.TrimSpace(t.Plugin)
	if !trackerPluginPattern.MatchString(t.Plugin) {
		return fmt.Errorf("config: tracker.plugin: expected a plugin name such as jira (runs wtui-tracker-<name>), got %q", t.Plugin)
	}
	if len(t.Queries) == 0 {
		t.Queries = []TrackerQuery{{Name: "My issues", Query: defaultTrackerQuery}}
	}
	for i := range t.Queries {
		q := &t.Queries[i]
		q.Name, q.Query = strings.TrimSpace(q.Name), strings.TrimSpace(q.Query)
		if q.Query == "" {
			return fmt.Errorf("config: tracker.queries[%d].query: expected a tracker query such as JQL", i)
		}
		if q.Name == "" {
			q.Name = fmt.Sprintf("Query %d", i+1)
		}
	}
	return nil
}
