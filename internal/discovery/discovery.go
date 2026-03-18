package discovery

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diss0x/wtui/internal/config"
	"github.com/diss0x/wtui/internal/domain"
	"github.com/diss0x/wtui/internal/git"
)

var ErrServiceNotFound = errors.New("service not found")

type Discoverer struct {
	cfg    *config.Config
	git    git.Client
	logger *slog.Logger
}

func New(cfg *config.Config, gitClient git.Client, logger *slog.Logger) *Discoverer {
	return &Discoverer{cfg: cfg, git: gitClient, logger: logger}
}

func (d *Discoverer) Resolve(ctx context.Context, token string) (string, error) {
	d.logger.DebugContext(ctx, "discovery: resolving token", slog.String("token", token), slog.String("root", d.cfg.RootDir))

	directPath := filepath.Join(d.cfg.RootDir, token)
	gitDir := filepath.Join(directPath, ".git")

	if info, statErr := statDir(gitDir); statErr == nil && info {
		d.logger.DebugContext(ctx, "discovery: found direct .git, validating", slog.String("path", directPath))
		if err := d.git.IsValidRepo(ctx, directPath); err != nil {
			return "", fmt.Errorf("discovery: direct repo at %s failed validation: %w", directPath, err)
		}
		return directPath, nil
	}

	var (
		found         string
		validationErr error
	)

	walkErr := filepath.WalkDir(d.cfg.RootDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == d.cfg.RootDir {
				return err
			}
			return fs.SkipDir
		}

		if path == d.cfg.RootDir {
			return nil
		}

		if !entry.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(d.cfg.RootDir, path)
		if relErr != nil {
			return fs.SkipDir
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1

		if depth > d.cfg.DiscoveryDepth {
			return fs.SkipDir
		}

		if entry.Name() == ".git" {
			if depth < 2 {
				return fs.SkipDir
			}

			parent := filepath.Dir(path)
			if filepath.Base(parent) == token {
				d.logger.DebugContext(ctx, "discovery: found .git match, validating",
					slog.String("parent", parent), slog.Int("depth", depth))
				if verr := d.git.IsValidRepo(ctx, parent); verr != nil {
					validationErr = fmt.Errorf("discovery: repo at %s failed validation: %w", parent, verr)
					return filepath.SkipAll
				}
				found = parent
				return filepath.SkipAll
			}
			return fs.SkipDir
		}

		if depth >= d.cfg.DiscoveryDepth {
			return fs.SkipDir
		}

		return nil
	})

	if validationErr != nil {
		return "", validationErr
	}
	if walkErr != nil {
		return "", fmt.Errorf("discovery: walk %s: %w", d.cfg.RootDir, walkErr)
	}
	if found != "" {
		return found, nil
	}

	return "", fmt.Errorf("%w: %s", ErrServiceNotFound, token)
}

// FindAll walks cfg.RootDir from depth 1 to cfg.DiscoveryDepth and returns every
// directory that contains a .git subdirectory. The check is a fast filesystem stat —
// git.IsValidRepo is NOT called, making this suitable for populating list views where
// low latency matters more than strict validation.
//
// Results are sorted alphabetically by repository name (filepath.Base of the path).
func (d *Discoverer) FindAll(ctx context.Context) ([]domain.Repo, error) {
	d.logger.DebugContext(ctx, "discovery: FindAll", slog.String("root", d.cfg.RootDir), slog.Int("depth", d.cfg.DiscoveryDepth))

	var repos []domain.Repo

	walkErr := filepath.WalkDir(d.cfg.RootDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == d.cfg.RootDir {
				return err
			}
			return fs.SkipDir
		}

		if path == d.cfg.RootDir {
			return nil
		}

		if !entry.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(d.cfg.RootDir, path)
		if relErr != nil {
			return fs.SkipDir
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1

		if depth > d.cfg.DiscoveryDepth {
			return fs.SkipDir
		}

		if entry.Name() == ".git" {
			if depth >= 2 {
				parent := filepath.Dir(path)
				repos = append(repos, domain.Repo{
					Name: filepath.Base(parent),
					Path: parent,
				})
			}
			return fs.SkipDir
		}

		if depth >= d.cfg.DiscoveryDepth {
			return fs.SkipDir
		}

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("discovery: walk %s: %w", d.cfg.RootDir, walkErr)
	}

	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Name < repos[j].Name
	})

	return repos, nil
}

func statDir(path string) (isDir bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}
	return info.IsDir(), nil
}
