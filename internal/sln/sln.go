package sln

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
	"github.com/D1ssolve/wtui/internal/dotnet"
)

type Manager struct {
	dotnet dotnet.Client
	logger *slog.Logger
}

func NewManager(dotnetClient dotnet.Client, logger *slog.Logger) *Manager {
	return &Manager{
		dotnet: dotnetClient,
		logger: logger,
	}
}

func (m *Manager) Generate(ctx context.Context, taskDir, taskID string, services []domain.Service) error {
	if !m.dotnet.IsAvailable(ctx) {
		m.logger.WarnContext(ctx, "dotnet CLI not found. Skipping .sln generation.")
		return nil
	}

	slnFileName := taskID + ".sln"
	slnPath := filepath.Join(taskDir, slnFileName)
	if err := os.Remove(slnPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logger.WarnContext(ctx, "failed to remove existing .sln file",
			slog.String("path", slnPath),
			slog.String("error", err.Error()),
		)
	}

	if err := m.dotnet.NewSln(ctx, taskDir, taskID); err != nil {
		m.logger.ErrorContext(ctx, "failed to create .sln file",
			slog.String("error", err.Error()),
		)
		return nil
	}

	for _, svc := range services {
		projs, err := findCsprojFiles(svc.WorktreePath)
		if err != nil {
			m.logger.WarnContext(ctx, "error walking service worktree for .csproj files",
				slog.String("service", svc.Name),
				slog.String("worktree", svc.WorktreePath),
				slog.String("error", err.Error()),
			)
			continue
		}

		if len(projs) == 0 {
			m.logger.WarnContext(ctx, "no .csproj files found for service",
				slog.String("service", svc.Name),
				slog.String("worktree", svc.WorktreePath),
			)
			continue
		}

		for _, projPath := range projs {
			if err := m.dotnet.SlnAdd(ctx, taskDir, slnFileName, projPath); err != nil {
				m.logger.WarnContext(ctx, "failed to add .csproj to solution",
					slog.String("service", svc.Name),
					slog.String("project", projPath),
					slog.String("error", err.Error()),
				)
			}
		}
	}

	return nil
}

func findCsprojFiles(root string) ([]string, error) {
	var matches []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		base := filepath.Base(path)
		if strings.HasSuffix(base, ".csproj") {
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				abs = path
			}
			matches = append(matches, abs)
		}
		return nil
	})

	return matches, err
}
