package task

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tracker"
)

type fakeTracker struct {
	calls []string
	err   error
}

func (f *fakeTracker) ListIssues(context.Context, string, int) ([]tracker.Issue, error) {
	return nil, nil
}

func (f *fakeTracker) GetIssue(context.Context, string) (tracker.IssueDetail, error) {
	return tracker.IssueDetail{}, nil
}

func (f *fakeTracker) Transition(_ context.Context, key, status string) error {
	f.calls = append(f.calls, "transition "+key+" "+status)
	return f.err
}

func (f *fakeTracker) Comment(_ context.Context, key, body string) error {
	f.calls = append(f.calls, "comment "+key+" "+body)
	return f.err
}

func trackerManager(ft *fakeTracker, commentMRs bool) *manager {
	cfg := &config.Config{Tracker: &config.TrackerConfig{
		Plugin:      "jira",
		CommentMRs:  commentMRs,
		Transitions: config.TrackerTransitions{TaskCreated: "In Progress", MROpened: "Code Review"},
	}}
	return &manager{cfg: cfg, tracker: ft, logger: slog.New(slog.DiscardHandler)}
}

func TestSyncTracker_JiraIssue_TransitionsAndComments(t *testing.T) {
	ft := &fakeTracker{}
	m := trackerManager(ft, true)
	issue := &domain.Issue{Provider: domain.IssueJira, Key: "WT-1"}
	m.syncTracker(context.Background(), issue, onMROpened, []string{"https://g/mr/1", "https://g/mr/2"}, nil)
	want := []string{"transition WT-1 Code Review", "comment WT-1 Merge requests:\nhttps://g/mr/1\nhttps://g/mr/2"}
	if strings.Join(ft.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %q, want %q", ft.calls, want)
	}
}

func TestSyncTracker_SkipsWhenNothingToDo(t *testing.T) {
	for name, tc := range map[string]struct {
		issue *domain.Issue
		pick  func(config.TrackerTransitions) string
		mrs   []string
	}{
		"no issue":          {nil, onTaskCreated, nil},
		"forge issue":       {&domain.Issue{Provider: domain.IssueGitHub, Key: "1"}, onTaskCreated, nil},
		"unmapped event":    {&domain.Issue{Provider: domain.IssueJira, Key: "WT-1"}, onMRMerged, nil},
		"comments disabled": {&domain.Issue{Provider: domain.IssueJira, Key: "WT-1"}, onMRMerged, []string{"u"}},
	} {
		t.Run(name, func(t *testing.T) {
			ft := &fakeTracker{}
			trackerManager(ft, false).syncTracker(context.Background(), tc.issue, tc.pick, tc.mrs, nil)
			if len(ft.calls) != 0 {
				t.Fatalf("calls = %q, want none", ft.calls)
			}
		})
	}
}

func TestSyncTracker_Failure_WarnsWithoutFailing(t *testing.T) {
	ft := &fakeTracker{err: errors.New("no transition to \"In Progress\"")}
	ch := make(chan string, 4)
	trackerManager(ft, false).syncTracker(context.Background(), &domain.Issue{Provider: domain.IssueJira, Key: "WT-1"}, onTaskCreated, nil, ch)
	close(ch)
	var lines []string
	for l := range ch {
		lines = append(lines, l)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "WT-1: move to In Progress failed") || !strings.Contains(lines[0], "update the issue manually") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestManager_TrackerIssues_RequireConfig(t *testing.T) {
	m := &manager{cfg: &config.Config{}}
	if _, err := m.ListTrackerIssues(context.Background(), "x"); !errors.Is(err, ErrTrackerNotConfigured) {
		t.Fatalf("err = %v", err)
	}
}
