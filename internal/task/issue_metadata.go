package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/fsutil"
)

const taskMetadataFileName = ".wtui-task.json"

type taskMetadata struct {
	Issue *domain.Issue `json:"issue"`
}

func writeTaskIssue(dir string, issue *domain.Issue) error {
	data, err := json.Marshal(taskMetadata{Issue: issue})
	if err == nil {
		err = fsutil.WriteFileAtomic(filepath.Join(dir, taskMetadataFileName), data, 0o644)
	}
	if err != nil {
		return fmt.Errorf("write task issue metadata: %w", err)
	}
	return nil
}

func (m *manager) taskIssue(ctx context.Context, dir string) *domain.Issue {
	data, err := os.ReadFile(filepath.Join(dir, taskMetadataFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var metadata taskMetadata
	if err == nil {
		err = json.Unmarshal(data, &metadata)
	}
	if err != nil {
		m.logger.WarnContext(ctx, "could not read task issue metadata", slog.String("task_dir", dir), slog.String("error", err.Error()))
		return nil
	}
	return metadata.Issue
}

func (m *manager) withIssueReference(params forge.CreateMRParams, issue *domain.Issue, remote string) forge.CreateMRParams {
	if ref := forge.IssueReference(issue, remote, m.cfg.Forge); ref != "" {
		body := strings.TrimRight(params.Description, " \t\r\n")
		if body != "" {
			body += "\n\n"
		}
		params.Description = body + ref
	}
	return params
}
