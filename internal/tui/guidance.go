package tui

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/execerr"
	"github.com/D1ssolve/wtui/internal/forge"
	"github.com/D1ssolve/wtui/internal/task"
)

// Domain errors carry their own instruction: "(press F to retry)", "; press C ..." or, from AI providers, ": run codex login ...".
var (
	hostnamePattern = `[a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)*`
	forgeLoginHost  = regexp.MustCompile(`^` + hostnamePattern + `$`)
	forgeLoginHint  = regexp.MustCompile(`(?im)(?:^|\n|; |: )(?:to get started[^\n]*?, please run:[ \t]*|run[ \t]+)?\b((?:gh|glab) auth login(?: --hostname ` + hostnamePattern + `)?)\b`)
	embeddedKeyHint = regexp.MustCompile(`\s*\((press [^)]+)\)`)
	instructionVerb = regexp.MustCompile(`^(?:press|run|use|restore|review|inspect|finish|merge|check|refresh|fix|delete|reject|install|ask|generate|try|close|open) (\S+)`)
)

func embeddedInstruction(text, sep string) (int, bool) {
	for i := 0; ; {
		j := strings.Index(text[i:], sep)
		if j < 0 {
			return 0, false
		}
		i += j
		if m := instructionVerb.FindStringSubmatch(text[i+len(sep):]); m != nil && m[1] != "failed" && m[1] != "failed." {
			return i, true
		}
		i += len(sep)
	}
}

type guidanceContext struct {
	flow    bool
	fileLog bool
}

func guidance(err error, at guidanceContext) (cause, action string) {
	cause = err.Error()
	var execErr *execerr.ExecError
	hasExec := errors.As(err, &execErr)
	var forgeErr *forge.ForgeError
	hasForge := errors.As(err, &forgeErr)

	if hasForge && forgeErr.Cause != nil {
		if stderr := strings.TrimSpace(forgeErr.Stderr); stderr != "" && strings.Contains(forgeErr.Cause.Error(), stderr) {
			cause = strings.Replace(cause, forgeErr.Error(), strings.TrimSuffix(forgeErr.Error(), ": "+stderr), 1)
		}
	}
	// A command in stored stderr may be beyond the viewport or on a line we condense.
	// Move only the recognized login instruction; the log still gets the original error.
	login := ""
	if hasForge || (hasExec && forgeCLI(err) != "") {
		if hint := forgeLoginHint.FindStringSubmatch(cause); hint != nil {
			login = hint[1]
		}
	}
	condensed := false
	if hasExec {
		if first, _, multi := strings.Cut(strings.TrimSpace(execErr.Stderr), "\n"); multi {
			short := strings.TrimSuffix(execErr.Error(), strings.TrimSpace(execErr.Stderr)) + strings.TrimSpace(first) + " …"
			cause, condensed = strings.Replace(cause, execErr.Error(), short, 1), true
		}
	}

	if login != "" {
		return strings.TrimRight(forgeLoginHint.ReplaceAllString(cause, ""), " .;:"), "Run " + login + ", then retry."
	}
	if loc := embeddedKeyHint.FindStringSubmatchIndex(cause); loc != nil {
		return cause[:loc[0]] + cause[loc[1]:], sentence(cause[loc[2]:loc[3]])
	}
	// External stderr is not ours to parse for instructions.
	if !hasExec && !hasForge {
		seps := []string{"; "}
		if errors.Is(err, ai.ErrUnavailable) || errors.Is(err, ai.ErrAuthentication) || errors.Is(err, ai.ErrUsageLimit) || errors.Is(err, ai.ErrInvalidResponse) {
			seps = append(seps, ": ")
		}
		best := -1
		for _, sep := range seps {
			if i, ok := embeddedInstruction(cause, sep); ok && (best < 0 || i < best) {
				best = i
			}
		}
		if best >= 0 {
			instruction, rest, _ := strings.Cut(cause[best+2:], "\n")
			if rest != "" {
				rest = "\n" + rest
			}
			return cause[:best] + rest, sentence(instruction)
		}
	}
	action = kindAction(err, at, condensed)
	return cause, action
}

func kindAction(err error, at guidanceContext, condensed bool) string {
	var execErr *execerr.ExecError
	var forgeErr *forge.ForgeError
	var missingTool *exec.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "Press r to refresh and check what was saved."
	case errors.Is(err, context.DeadlineExceeded):
		return "Check the network connection and the remote host, then retry."
	case errors.As(err, &missingTool) && errors.Is(missingTool.Err, exec.ErrNotFound):
		return "Install " + filepath.Base(missingTool.Name) + " or add it to PATH, then retry."
	case errors.Is(err, forge.ErrForgeUnavailable) || errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryNotInstalled:
		return "Install gh (GitHub) or glab (GitLab) and add it to PATH, then retry."
	case errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryAuthError, forgeAuthFailure(err):
		cli, host := forgeCLI(err), ""
		if forgeErr != nil {
			switch forgeErr.Provider {
			case forge.ForgeProviderGitLab:
				cli, host = "glab", forgeErr.Host
			case forge.ForgeProviderGitHub:
				cli = "gh"
				if forgeErr.Host != "github.com" {
					host = forgeErr.Host
				}
			}
		}
		if cli == "" {
			return "Check authentication for this forge, then retry."
		}
		command := cli + " auth login"
		if forgeLoginHost.MatchString(host) {
			command += " --hostname " + host
		}
		return "Run " + command + ", then retry."
	case errors.As(err, &forgeErr) && forgeErr.Category == forge.ErrCategoryNetwork:
		return "Check the network connection to the forge host, then retry."
	case errors.Is(err, task.ErrReleaseFlowChanged):
		return "Restore the release configuration it started with, or reject the release and prepare it again."
	case errors.Is(err, task.ErrReleaseMasterMoved):
		return "Inspect the release branch history, then reject the release and prepare it again."
	case errors.Is(err, task.ErrReleaseRetryUnsafe):
		return "Restore the branches and worktrees named above, or reject the release and prepare it again."
	case errors.Is(err, task.ErrTaskFlowStep) && at.flow:
		return "Press W to see which events the current state accepts."
	case errors.Is(err, task.ErrServiceNotFound):
		return "Press r to refresh, then select the service again."
	case configError(err):
		return "Fix the config value named above, check it with wtui flow, then restart wtui."
	case errors.Is(err, fs.ErrNotExist):
		return "Check that the path exists, then press r to refresh."
	case errors.As(err, &execErr) && filepath.Base(firstArg(execErr.Argv)) == "git":
		if condensed {
			return diagnosticAction(at.fileLog, "the full git output, then fix the repository and retry.")
		}
		return "Fix the repository problem shown above, then retry."
	default:
		return ""
	}
}

// configError matches internal/config errors, which carry a "config: " prefix rather than a type.
func configError(err error) bool {
	text := err.Error()
	return strings.HasPrefix(text, "config: ") || strings.Contains(text, ": config: ")
}

func forgeAuthFailure(err error) bool {
	var execErr *execerr.ExecError
	if !errors.As(err, &execErr) || forgeCLI(err) == "" {
		return false
	}
	stderr := strings.ToLower(execErr.Stderr)
	return strings.Contains(stderr, "auth login") || strings.Contains(stderr, "401") || strings.Contains(stderr, "not logged")
}

func forgeCLI(err error) string {
	var execErr *execerr.ExecError
	if !errors.As(err, &execErr) {
		return ""
	}
	switch cli := filepath.Base(firstArg(execErr.Argv)); cli {
	case "gh", "glab":
		return cli
	}
	return ""
}

func firstArg(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return argv[0]
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s[:1]) + s[1:]
	if !strings.HasSuffix(s, ".") {
		s += "."
	}
	return s
}
