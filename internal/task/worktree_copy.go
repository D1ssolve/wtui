package task

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func (m *manager) copyLocalFiles(ctx context.Context, repoPath, dest string, statusCh chan<- string) {
	if m == nil || m.cfg == nil || m.cfg.Worktree == nil || len(m.cfg.Worktree.Copy) == 0 {
		return
	}

	files, err := m.git.ListLocalFiles(ctx, repoPath)
	if err != nil {
		m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("failed to list local files in %s: %v", repoPath, err))
		return
	}
	sourceRoot, err := os.OpenRoot(repoPath)
	if err != nil {
		m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("failed to open source repository %s: %v", repoPath, err))
		return
	}
	defer sourceRoot.Close()
	destRoot, err := os.OpenRoot(dest)
	if err != nil {
		m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("failed to open worktree %s: %v", dest, err))
		return
	}
	defer destRoot.Close()

	for _, relative := range files {
		matched := false
		for _, pattern := range m.cfg.Worktree.Copy {
			if ok, matchErr := doublestar.Match(pattern, filepath.ToSlash(relative)); matchErr == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		cleanRelative, ok := safeLocalRelativePath(relative)
		if !ok {
			m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("skipping unsafe local file path %q", relative))
			continue
		}
		sourcePath := filepath.Join(repoPath, cleanRelative)

		info, statErr := sourceRoot.Lstat(cleanRelative)
		if statErr != nil {
			m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("failed to inspect local file %s: %v", sourcePath, statErr))
			continue
		}
		if !info.Mode().IsRegular() {
			m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("skipping non-regular local file %s", sourcePath))
			continue
		}

		if copyErr := copyRegularFile(sourceRoot, destRoot, cleanRelative, info.Mode().Perm()); copyErr != nil {
			m.warnLocalFileCopy(ctx, statusCh, fmt.Sprintf("failed to copy local file %s: %v", sourcePath, copyErr))
		}
	}
}

func safeLocalRelativePath(relative string) (string, bool) {
	native := filepath.FromSlash(relative)
	if native == "" || filepath.IsAbs(native) || filepath.VolumeName(native) != "" {
		return "", false
	}
	clean := filepath.Clean(native)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

func copyRegularFile(sourceRoot, destRoot *os.Root, relative string, mode os.FileMode) error {
	if err := destRoot.MkdirAll(filepath.Dir(relative), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}

	source, err := sourceRoot.Open(relative)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer source.Close()

	destination, err := destRoot.OpenFile(relative, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	keepDestination := false
	defer func() {
		if !keepDestination {
			_ = destination.Close()
			_ = destRoot.Remove(relative)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	if err := destination.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}
	if err := destRoot.Chmod(relative, mode); err != nil {
		return fmt.Errorf("preserve permissions: %w", err)
	}
	keepDestination = true
	return nil
}

func (m *manager) warnLocalFileCopy(ctx context.Context, statusCh chan<- string, detail string) {
	message := "Warning: " + detail
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, detail)
	sendStatus(statusCh, message)
}
