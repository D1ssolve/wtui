package task

import (
	"errors"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
)

// recoveryError keeps guard diagnostics intact while carrying cause-site advice.
type recoveryError struct {
	err         error
	action      string
	explanation string
}

func (e *recoveryError) Error() string { return e.err.Error() }
func (e *recoveryError) Unwrap() error { return e.err }

func withRecovery(err error, action string) error {
	return &recoveryError{err: err, action: action}
}

func workflowRecovery(err error, fallback string) (string, string) {
	var forgeErr *forge.ForgeError
	if errors.As(err, &forgeErr) {
		switch forgeErr.Category {
		case forge.ErrCategoryNotInstalled:
			return "Forge CLI is not installed", "install the forge CLI; refresh workflow"
		case forge.ErrCategoryAuthError:
			return "Forge authentication failed", "authenticate the forge CLI; refresh workflow"
		case forge.ErrCategoryNetwork:
			return "Could not reach the forge", "check network and forge access; refresh workflow"
		case forge.ErrCategoryParseError:
			return "Forge returned unreadable MR data", "inspect forge CLI JSON/version; refresh workflow"
		}
	}
	if errors.Is(err, forge.ErrForgeUnavailable) {
		return "Forge CLI is unavailable", "install/configure the forge CLI and provider; refresh"
	}
	explanation := "Could not verify review MR; see ? help for diagnostics"
	var guided *recoveryError
	if errors.As(err, &guided) {
		if guided.explanation != "" {
			explanation = guided.explanation
		}
		return explanation, guided.action
	}
	return explanation, fallback
}

func failedWorkflowInspection(item ServiceMergeInspection, err error, action string) ServiceMergeInspection {
	item.Status = "failed"
	item.Blockers = []string{"Could not verify review MR: " + err.Error()}
	item.Explanation, item.NextAction = workflowRecovery(err, action)
	return item
}

func reviewInspectionAction(item ServiceMergeInspection) string {
	if item.NextAction != "" {
		return item.NextAction
	}
	if item.MR.NextAction != "" {
		return item.MR.NextAction
	}
	switch item.Status {
	case "failed":
		return "inspect MR and repository diagnostics; refresh workflow"
	case "blocked":
		return "open the MR in forge; resolve its merge requirements"
	case "waiting":
		return "open review/checks in forge; satisfy requirements"
	default:
		return ""
	}
}

// workflowBlockerActions keeps each action tied to its service. Hotfix rows
// already include the target, so equal actions for different targets survive.
func workflowBlockerActions(rows []domain.ServiceWorkflow) string {
	var actions []string
	names := make(map[string][]string)
	for _, row := range rows {
		if (row.Blocker == "" && row.Status != "waiting") || row.NextAction == "" {
			continue
		}
		if _, exists := names[row.NextAction]; !exists {
			actions = append(actions, row.NextAction)
		}
		names[row.NextAction] = appendUnique(names[row.NextAction], row.ServiceName)
	}
	for i, action := range actions {
		actions[i] = strings.Join(names[action], ", ") + ": " + action
	}
	return strings.Join(actions, "; ")
}
