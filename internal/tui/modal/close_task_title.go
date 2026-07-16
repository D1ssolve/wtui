package modal

import (
	"fmt"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

func closeTaskModalTitle(taskInfo domain.Task, fallbackTaskID string) string {
	taskID := strings.TrimSpace(taskInfo.ID)
	if taskID == "" {
		taskID = strings.TrimSpace(fallbackTaskID)
	}

	phase := strings.ToLower(strings.TrimSpace(taskInfo.Phase))
	version := strings.TrimSpace(taskInfo.Version)

	switch phase {
	case "feature":
		return fmt.Sprintf("Close Feature Task: %s", taskID)
	case "release":
		if version != "" {
			return fmt.Sprintf("Close Release Task: %s (v%s)", taskID, version)
		}
		return fmt.Sprintf("Close Release Task: %s", taskID)
	case "hotfix":
		if version != "" {
			return fmt.Sprintf("Close Hotfix Task: %s (v%s)", taskID, version)
		}
		return fmt.Sprintf("Close Hotfix Task: %s", taskID)
	default:
		return fmt.Sprintf("Close Task: %s", taskID)
	}
}
