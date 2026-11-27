package task

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/tracker"
)

var ErrTrackerNotConfigured = errors.New("tracker is not configured; add a tracker: section with plugin: jira")

func (m *manager) ListTrackerIssues(ctx context.Context, query string) ([]tracker.Issue, error) {
	if m.tracker == nil {
		return nil, ErrTrackerNotConfigured
	}
	return m.tracker.ListIssues(ctx, query, 50)
}

func (m *manager) GetTrackerIssue(ctx context.Context, key string) (tracker.IssueDetail, error) {
	if m.tracker == nil {
		return tracker.IssueDetail{}, ErrTrackerNotConfigured
	}
	return m.tracker.GetIssue(ctx, key)
}

// syncTracker mirrors a finished wtui operation into the task's issue. It is
// best effort: the git/forge work already happened, so failures only warn.
func (m *manager) syncTracker(ctx context.Context, issue *domain.Issue, pick func(config.TrackerTransitions) string, mrURLs []string, statusCh chan<- string) {
	if m.tracker == nil || issue == nil || issue.Provider != domain.IssueJira {
		return
	}
	warn := func(action string, err error) {
		m.logger.WarnContext(ctx, "tracker sync failed", slog.String("issue", issue.Key), slog.String("action", action), slog.String("error", err.Error()))
		if statusCh != nil {
			sendLine(ctx, statusCh, "[tracker] "+issue.Key+": "+action+" failed: "+err.Error()+"; update the issue manually")
		}
	}
	cfg := m.cfg.Tracker
	if status := pick(cfg.Transitions); status != "" {
		if err := m.tracker.Transition(ctx, issue.Key, status); err != nil {
			warn("move to "+status, err)
		} else if statusCh != nil {
			sendLine(ctx, statusCh, "[tracker] "+issue.Key+" → "+status)
		}
	}
	if cfg.CommentMRs && len(mrURLs) > 0 {
		if err := m.tracker.Comment(ctx, issue.Key, "Merge requests:\n"+strings.Join(mrURLs, "\n")); err != nil {
			warn("comment", err)
		}
	}
}

func onTaskCreated(t config.TrackerTransitions) string { return t.TaskCreated }
func onMROpened(t config.TrackerTransitions) string    { return t.MROpened }
func onMRMerged(t config.TrackerTransitions) string    { return t.MRMerged }
