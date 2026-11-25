package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

func (m *manager) ProposeReleaseVersions(ctx context.Context, taskIDs []string) (map[string]string, error) {
	versions := make(map[string]string)
	type latestTag struct {
		name    string
		version *semver.Version
	}
	repoLatest := make(map[string]latestTag)
	seenTasks := make(map[string]struct{})

	for _, taskID := range taskIDs {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			continue
		}
		if _, seen := seenTasks[taskID]; seen {
			continue
		}
		seenTasks[taskID] = struct{}{}

		services, err := m.ListServices(ctx, taskID)
		if err != nil {
			return nil, fmt.Errorf("propose release versions for task %s: %w", taskID, err)
		}
		for _, svc := range services {
			name := strings.TrimSpace(svc.Name)
			if name == "" {
				continue
			}

			repoPath := strings.TrimSpace(svc.RepoPath)
			latest, scanned := repoLatest[repoPath]
			if repoPath != "" && !scanned {
				tags, err := m.git.ListTags(ctx, repoPath)
				if err != nil {
					return nil, fmt.Errorf("propose release version for service %s: %w", name, err)
				}
				for _, tag := range tags {
					if tag.IsSemver && tag.Version != nil && (latest.version == nil || latest.version.LessThan(tag.Version)) {
						latest = latestTag{tag.Name, tag.Version}
					}
				}
				repoLatest[repoPath] = latest
			}

			version, err := m.nextVersion(ctx, repoPath, latest.name, latest.version, "origin/"+m.integrationBranch())
			if err != nil {
				return nil, fmt.Errorf("propose release version for service %s: %w", name, err)
			}
			versions[name] = version
		}
	}

	return versions, nil
}
