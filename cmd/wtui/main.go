package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/app"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
	"github.com/D1ssolve/wtui/internal/logutil"
	"github.com/D1ssolve/wtui/internal/tui"
)

// Version is set via -ldflags at release build time.
// When installed via go install, resolveVersion falls back to module info.
var Version = ""

// resolveVersion returns the ldflag-injected version if set,
// otherwise falls back to the module version from runtime/debug.ReadBuildInfo
// (available when installed via go install).
func resolveVersion() string {
	if Version != "" {
		return Version
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func main() {
	if versionRequested(os.Args[1:]) {
		fmt.Print(versionOutput(resolveVersion()))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "flow" {
		format := ""
		if len(os.Args) > 2 {
			format = os.Args[2]
		}
		var out string
		var err error
		switch {
		case format == "--draft" && len(os.Args) == 4 && strings.TrimSpace(os.Args[3]) != "":
			out, err = draftCommand(os.Args[3])
		case format == "" || format == "--mermaid" || format == "--html":
			out, err = flowCommand(format)
		default:
			fmt.Fprintln(os.Stderr, "usage: wtui flow [--mermaid | --html | --draft \"<how your team works>\"]")
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "wtui flow:", err)
			os.Exit(1)
		}
		fmt.Print(out)
		return
	}

	if err := runTUI(); err != nil {
		slog.Error("TUI failed", "err", err)
		os.Exit(1)
	}
}

func versionRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--version" || arg == "-v" {
			return true
		}
	}

	return false
}

func versionOutput(version string) string {
	return fmt.Sprintf("wtui %s\n", version)
}

// flowCommand validates the configured workflow and describes what wtui will do with it.
func flowCommand(format string) (string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return "", err
	}
	if cfg, err = cfg.Effective(); err != nil {
		return "", err
	}
	flow, err := gitflow.Resolve(cfg)
	if err != nil {
		return "", err
	}
	switch format {
	case "--mermaid":
		return flow.Mermaid(), nil
	case "--html":
		return flow.HTML(), nil
	}
	return flow.Describe(), nil
}

// draftCommand asks the configured AI provider for a workflow block and keeps it only once
// the workflow compiler accepts it, feeding validation errors back for a bounded number of fixes.
func draftCommand(description string) (string, error) {
	cfg, err := config.Load("")
	if err != nil {
		return "", err
	}
	if cfg, err = cfg.Effective(); err != nil {
		return "", err
	}
	assistant, err := app.BuildAssistant(cfg.AI)
	if err != nil {
		return "", err
	}
	return draftWorkflow(context.Background(), assistant, description)
}

const draftAttempts = 3

func draftWorkflow(ctx context.Context, drafter interface {
	DraftWorkflow(context.Context, ai.WorkflowRequest) (string, error)
}, description string) (string, error) {
	request := ai.WorkflowRequest{Description: description, Reference: gitflow.FormatReference()}
	for attempt := 1; ; attempt++ {
		draft, err := drafter.DraftWorkflow(ctx, request)
		if err != nil {
			return "", err
		}
		flow, err := gitflow.ParseWorkflowYAML(draft)
		if err == nil {
			var b strings.Builder
			b.WriteString("# Drafted by AI from your description and checked by wtui. Review before use.\n")
			for line := range strings.SplitSeq(strings.TrimRight(flow.Describe(), "\n"), "\n") {
				b.WriteString("# " + line + "\n")
			}
			b.WriteString(draft)
			return b.String(), nil
		}
		if attempt == draftAttempts {
			return "", fmt.Errorf("the AI draft still fails validation after %d attempts: %v", draftAttempts, err)
		}
		request.Previous, request.Problem = draft, err.Error()
	}
}

func runTUI() error {
	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg, err = cfg.Effective()
	if err != nil {
		return fmt.Errorf("normalize config: %w", err)
	}

	logLevel := new(slog.LevelVar)
	logLevel.Set(logutil.ParseLogLevel(cfg.LogLevel))
	logger, logErr := logutil.InitLogger("wtui", logLevel)
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open log file: %v\n", logErr)
		logLevel = nil
	}

	deps, err := app.BuildDependencies(cfg, logger)
	if err != nil {
		return fmt.Errorf("build dependencies: %w", err)
	}

	model, err := tui.NewWithOptions(cfg, deps.Manager, logger, tui.Options{
		LazygitAvailable:  deps.LazygitAvailable,
		GlabAvailable:     deps.GlabAvailable,
		GhAvailable:       deps.GhAvailable,
		ResolvedFlow:      deps.ResolvedFlow,
		MetadataGenerator: deps.MetadataGenerator,
		Assistant:         deps.Assistant,
		Version:           resolveVersion(),
		LogLevel:          logLevel,
	})
	if err != nil {
		return fmt.Errorf("create TUI model: %w", err)
	}

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
