package validation

import (
	"strings"

	"github.com/D1ssolve/wtui/internal/domain"
)

var conflictXY = map[string]struct{}{
	"DD": {}, "AU": {}, "UD": {}, "UA": {}, "DU": {}, "AA": {}, "UU": {},
}

var dirtyX = map[byte]struct{}{
	'M': {}, 'A': {}, 'D': {}, 'R': {}, 'C': {},
}

var dirtyY = map[byte]struct{}{
	'M': {}, 'D': {},
}

func ParsePorcelainV2(output string) (states []domain.RepoState, changedCount int, untrackedCount int, conflictPaths []string) {
	var hasDirty bool
	var hasUntracked bool
	var hasConflicts bool

	for _, rawRecord := range strings.Split(output, "\x00") {
		for _, rawLine := range strings.Split(rawRecord, "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			xy, path, ok := parseXYAndPath(line)
			if !ok {
				continue
			}

			switch xy {
			case "!!":
				continue
			case "??":
				hasUntracked = true
				untrackedCount++
				continue
			}

			if _, isConflict := conflictXY[xy]; isConflict {
				hasConflicts = true
				conflictPaths = append(conflictPaths, path)
			}

			if isTrackedDirty(xy) {
				hasDirty = true
				changedCount++
			}
		}
	}

	if hasDirty {
		states = append(states, domain.RepoStateDirty)
	}
	if hasUntracked {
		states = append(states, domain.RepoStateUntracked)
	}
	if hasConflicts {
		states = append(states, domain.RepoStateConflicted)
	}
	if len(states) == 0 {
		states = append(states, domain.RepoStateClean)
	}

	return states, changedCount, untrackedCount, conflictPaths
}

func parseXYAndPath(line string) (xy, path string, ok bool) {
	if strings.HasPrefix(line, "?? ") {
		return "??", strings.TrimSpace(strings.TrimPrefix(line, "?? ")), true
	}
	if strings.HasPrefix(line, "!! ") {
		return "!!", strings.TrimSpace(strings.TrimPrefix(line, "!! ")), true
	}
	if strings.HasPrefix(line, "? ") {
		return "??", strings.TrimSpace(strings.TrimPrefix(line, "? ")), true
	}
	if strings.HasPrefix(line, "! ") {
		return "!!", strings.TrimSpace(strings.TrimPrefix(line, "! ")), true
	}

	if strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") || strings.HasPrefix(line, "u ") {
		parts := strings.Fields(line)
		if len(parts) < 3 {
			return "", "", false
		}
		xy = parts[1]
		path = parts[len(parts)-1]
		if idx := strings.Index(path, "\t"); idx >= 0 {
			path = path[:idx]
		}
		return xy, path, len(xy) == 2
	}

	if len(line) >= 3 && line[2] == ' ' {
		xy = line[:2]
		path = strings.TrimSpace(line[3:])
		return xy, path, len(xy) == 2
	}

	return "", "", false
}

func isTrackedDirty(xy string) bool {
	if len(xy) != 2 {
		return false
	}
	_, xDirty := dirtyX[xy[0]]
	_, yDirty := dirtyY[xy[1]]
	return xDirty || yDirty
}
