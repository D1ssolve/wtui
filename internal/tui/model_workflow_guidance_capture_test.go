package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/D1ssolve/wtui/internal/domain"
)

// These fixtures also capture the pre-fix duplicate and hidden recovery action.
func TestCaptureWorkflowGuidance(t *testing.T) {
	dir := os.Getenv("WTUI_GUIDANCE_CAPTURE_DIR")
	if dir == "" {
		t.Skip("WTUI_GUIDANCE_CAPTURE_DIR not set")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	cases := []struct {
		name   string
		kind   string
		reason string
		action string
	}{
		{"changed-source", "task", "MR !428 covers a different source commit (merged d4275572, current 536f1142)", "compare feature/TASK-1 with develop; create and merge a follow-up MR for intended remaining changes, then press r"},
		{"waiting", "task", "not approved", "Request approval from a reviewer, then press r to refresh."},
		{"ready", "task", "", "merge ready MRs in forge, then press M to reconcile"},
		{"failed-release", "release", "pipeline failed", "Inspect the failed job, fix its cause, then retry the release."},
		{"hotfix-targets", "hotfix", "master: conflicts", "master: Resolve conflicts with the MR target, push the resolution, then press r."},
		{"grouped-recovery", "group", "forge CLI unavailable", "install the forge CLI; refresh workflow"},
		{"load-failure", "failure", "", ""},
		{"help-recovery", "help", "MR !428 covers a different source commit", "compare source with develop; merge intended remaining changes, then press r"},
		{"help-recovery-scrolled", "help-scrolled", "MR !428 covers a different source commit", "compare source with develop; merge intended remaining changes, then press r"},
		{"hotfix-target-help", "hotfix-help", "master: conflicts", "master: resolve source/target conflicts; publish the resolution"},
		{"hotfix-open-head", "hotfix", "master: MR !42 covers a different source commit (MR head d4275572, current 536f1142)", "master: inspect the existing MR in forge; compare its head with published hotfix/TASK-1 and refresh before retrying"},
		{"forge-auth-error", "task", "Forge authentication failed", "authenticate the forge CLI; refresh workflow"},
	}
	for _, tc := range cases {
		for _, size := range []struct{ width, height int }{{120, 40}, {60, 24}, {50, 10}} {
			name := tc.name + "-" + sizeLabel(size.width, size.height)
			t.Run(name, func(t *testing.T) {
				m := sendWindowSize(newTestModel(t, &mockManager{}), size.width, size.height)
				m = loadTasksForView(t, m)
				m.servicesPanel.SetServices("TASK-1", []domain.Service{{Name: "api", Branch: "feature/TASK-1"}})
				wf := testWorkflowSummary()
				wf.Steps = []domain.WorkflowStep{
					{Phase: domain.TaskWorkflowCode, Label: "code", State: "done"},
					{Phase: domain.TaskWorkflowMR, Label: "MR", State: "done"},
					{Phase: domain.TaskWorkflowReviewCI, Label: "review + CI", State: "blocked"},
					{Phase: domain.TaskWorkflowMerge, Label: "merge", State: "next"},
					{Phase: domain.TaskWorkflowReleaseEligible, Label: "release", State: "next"},
				}
				wf.NextAction = "api: " + tc.action
				if tc.reason != "" {
					wf.Blocker = "api: " + tc.reason
				}
				status := "blocked"
				switch tc.name {
				case "waiting":
					status = "waiting"
					wf.Steps[2].State = "now"
				case "ready":
					status = "ready"
					wf.Steps[2].State = "done"
					wf.Steps[3].State = "now"
				}
				wf.Services = []domain.ServiceWorkflow{{
					ServiceName: "api", Status: status, Detail: tc.reason,
					Blocker: tc.reason, NextAction: tc.action,
				}}
				if tc.kind == "hotfix" || tc.kind == "hotfix-help" {
					wf.Services = append(wf.Services, domain.ServiceWorkflow{
						ServiceName: "api", Status: "ready", Detail: "develop: ready",
						NextAction: "develop: merge the ready MR",
					})
				}
				if tc.kind == "group" {
					wf.Services = append(wf.Services, domain.ServiceWorkflow{
						ServiceName: "web", Status: "failed", Blocker: tc.reason, NextAction: tc.action,
					})
					wf.Blocker += "; web: " + tc.reason
					wf.NextAction = "api, web: " + tc.action
					m.servicesPanel.SetServices("TASK-1", []domain.Service{{Name: "api"}, {Name: "web"}})
				}
				if tc.kind == "release" {
					updated, _ := m.Update(ReleasesLoadedMsg{Releases: []domain.Release{{ID: "REL-1", Version: "1.2.3", Status: domain.ReleaseStatusFailed}}})
					m = updated.(Model)
					m.setFocus(FocusReleases)
					m.releaseWorkflow = &wf
					m.releasesPanel.SetWorkflow(&wf)
					m = sendWindowSize(m, size.width, size.height)
				} else {
					m = loadTaskWorkflow(t, m, wf)
					if tc.kind == "failure" {
						updated, _ := m.Update(TaskWorkflowLoadedMsg{
							TaskID: "TASK-1", Generation: 1, Err: errors.New("fetch origin failed: repository access denied"),
						})
						m = updated.(Model)
					}
					if tc.kind == "help" || tc.kind == "help-scrolled" || tc.kind == "hotfix-help" {
						m = openHelp(t, m)
						if tc.kind == "help-scrolled" {
							scrollSteps := 2
							if size.height < 12 {
								scrollSteps = 5
							}
							for range scrollSteps {
								updated, _ := m.Update(sendKey("j"))
								m = updated.(Model)
							}
						}
						if tc.kind == "hotfix-help" {
							for range 20 {
								if strings.Contains(stripANSIForModel(m.View()), "api next: develop:") {
									break
								}
								updated, _ := m.Update(sendKey("j"))
								m = updated.(Model)
							}
						}
					}
				}
				raw := m.View()
				plain := stripANSIForModel(raw)
				metadata := inspectCapture(name, plain, size.width, size.height, nil, nil)
				for _, check := range metadata.Checks {
					if !check.Passed {
						t.Errorf("capture check failed: %s", check.Name)
					}
				}
				if tc.kind == "hotfix-help" {
					if !strings.Contains(plain, "api next: develop:") {
						t.Fatalf("help must expose the separate develop action:\n%s", plain)
					}
				} else if tc.kind == "help" && size.height < 12 {
					if !strings.Contains(plain, "more below") || !strings.Contains(plain, "Task TASK-1:") {
						t.Fatalf("short help must retain task context and a scroll cue:\n%s", plain)
					}
				} else if tc.kind == "help-scrolled" {
					if !strings.Contains(plain, "Next:") {
						t.Fatalf("scrolling must reveal the recovery action:\n%s", plain)
					}
				} else if !strings.Contains(plain, "ⓘ") {
					t.Fatalf("workflow message not visible:\n%s", plain)
				}
				writeCaptureFile(t, filepath.Join(dir, name, "view.ansi.txt"), []byte(raw))
				writeCaptureFile(t, filepath.Join(dir, name, "view.txt"), []byte(plain))
				writeCaptureJSON(t, filepath.Join(dir, name, "metadata.json"), metadata)
			})
		}
	}
}
