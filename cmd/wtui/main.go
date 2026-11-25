package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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

const usageText = `usage:
  wtui [--config PATH]
  wtui flow [--mermaid | --html | --draft "<how your team works>"] [--config PATH]

options:
  --config PATH   use this config file instead of the default lookup
  -h, --help      show this help
  -v, --version   print the version
`

// cliArgs is the parsed command line.
type cliArgs struct {
	help, version bool
	flow          bool
	config        string
	format, draft string // flow output: "" (describe), "--mermaid" or "--html"; or a --draft description
}

// parseArgs accepts --config PATH or --config=PATH before or after the flow subcommand.
func parseArgs(args []string) (cliArgs, error) {
	var c cliArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			c.help = true
		case arg == "-v" || arg == "--version":
			c.version = true
		case arg == "flow" && !c.flow:
			c.flow = true
		case arg == "--config" || strings.HasPrefix(arg, "--config="):
			value, ok := strings.CutPrefix(arg, "--config=")
			if !ok {
				if i+1 >= len(args) {
					return c, errors.New("--config requires a path")
				}
				i++
				value = args[i]
			}
			if value == "" {
				return c, errors.New("--config requires a non-empty path")
			}
			c.config = value
		case c.flow && (arg == "--mermaid" || arg == "--html"):
			c.format = arg
		case c.flow && arg == "--draft":
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return c, errors.New("--draft requires a description of how your team works")
			}
			i++
			c.draft = args[i]
		default:
			return c, fmt.Errorf("unknown argument %q", arg)
		}
	}
	if c.version && (c.help || c.flow || c.config != "") {
		return c, errors.New("--version cannot be combined with other arguments")
	}
	return c, nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command line and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	c, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "wtui: %v\n%s", err, usageText)
		return 2
	}
	if c.version {
		fmt.Fprint(stdout, versionOutput(resolveVersion()))
		return 0
	}
	if c.help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if c.flow {
		var out string
		if c.draft != "" {
			out, err = draftCommand(c.draft, c.config)
		} else {
			out, err = flowCommand(c.format, c.config)
		}
		if err != nil {
			fmt.Fprintln(stderr, "wtui flow:", err)
			return 1
		}
		fmt.Fprint(stdout, out)
		return 0
	}
	if err := runTUI(c.config); err != nil {
		slog.Error("TUI failed", "err", err)
		return 1
	}
	return 0
}

func versionOutput(version string) string {
	return fmt.Sprintf("wtui %s\n", version)
}

// flowCommand validates the configured workflow and describes what wtui will do with it.
func flowCommand(format, configPath string) (string, error) {
	cfg, err := config.Load(configPath)
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
func draftCommand(description, configPath string) (string, error) {
	cfg, err := config.Load(configPath)
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

func runTUI(configPath string) error {
	cfg, err := config.Load(configPath)
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
