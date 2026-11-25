package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"slices"
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
  wtui [--config PATH] flow [--mermaid | --html | --draft "TEXT"]
  wtui [--config PATH] init [--root DIR] [--tasks-root DIR] [--editor CMD]
                       [--preset git-flow|github-flow|trunk-based] [--yes] [--force]
  wtui -h | --help
  wtui -v | --version
`

// cliArgs is the parsed command line.
type cliArgs struct {
	help, version bool
	flow          bool
	config        string
	format, draft string // flow output: "" (describe), "--mermaid" or "--html"; or a --draft description

	init                            bool
	root, tasksRoot, editor, preset string // init answers given as flags
	yes, force                      bool
}

// parseArgs reads the whole command line in one pass. An unknown argument wins over every other
// error; value flags (--config, --root, ...) take NAME VALUE or NAME=VALUE anywhere on the line,
// and the token after such a flag is always its value.
func parseArgs(args []string) (cliArgs, error) {
	var c cliArgs
	var bad error // first structural error; reported only if no unknown argument follows
	fail := func(msg string) {
		if bad == nil {
			bad = errors.New(msg)
		}
	}
	valueFlags := map[string]struct {
		dst  *string
		noun string
	}{
		"--config":     {&c.config, "path"},
		"--root":       {&c.root, "directory"},
		"--tasks-root": {&c.tasksRoot, "directory"},
		"--editor":     {&c.editor, "command"},
		"--preset":     {&c.preset, "preset name"},
	}
	switches := map[string]*bool{"--yes": &c.yes, "--force": &c.force}
	modes := 0    // how many of --mermaid, --html, --draft were given
	initOnly := 0 // how many init-only flags were given
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, inline := strings.Cut(arg, "=")
		if flag, ok := valueFlags[name]; ok {
			if name != "--config" {
				initOnly++
			}
			if *flag.dst != "" {
				fail(name + " given more than once")
			}
			if !inline {
				if i+1 >= len(args) {
					fail(fmt.Sprintf("%s requires a %s", name, flag.noun))
					continue
				}
				i++
				value = args[i]
			}
			if value == "" {
				fail(fmt.Sprintf("%s requires a non-empty %s", name, flag.noun))
			}
			*flag.dst = value
			continue
		}
		if sw, ok := switches[arg]; ok {
			initOnly++
			if *sw {
				fail(arg + " given more than once")
			}
			*sw = true
			continue
		}
		switch {
		case arg == "-h" || arg == "--help":
			c.help = true
		case arg == "-v" || arg == "--version":
			c.version = true
		case arg == "flow":
			if c.flow {
				fail("flow given more than once")
			}
			c.flow = true
		case arg == "init":
			if c.init {
				fail("init given more than once")
			}
			c.init = true
		case arg == "--mermaid" || arg == "--html":
			c.format = arg
			modes++
		case arg == "--draft":
			modes++
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				fail("--draft requires a description of how your team works")
				i++
				continue
			}
			i++
			c.draft = args[i]
		default:
			return c, fmt.Errorf("unknown argument %q", arg)
		}
	}
	if modes > 1 {
		fail("--mermaid, --html and --draft cannot be combined")
	}
	if modes > 0 && !c.flow {
		fail("--mermaid, --html and --draft only apply to the flow subcommand")
	}
	if c.init && c.flow {
		fail("init and flow cannot be combined")
	}
	if initOnly > 0 && !c.init {
		fail("--root, --tasks-root, --editor, --preset, --yes and --force only apply to the init subcommand")
	}
	if c.preset != "" && !slices.Contains(gitflow.PresetNames(), c.preset) {
		fail(fmt.Sprintf("--preset: unknown preset %q: expected one of %s", c.preset, strings.Join(gitflow.PresetNames(), ", ")))
	}
	return c, bad
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
	if c.help {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if c.version {
		fmt.Fprint(stdout, versionOutput(resolveVersion()))
		return 0
	}
	if c.init {
		return initCommand(c, os.Stdin, stdout, stderr, draftWithAssistant)
	}
	if c.flow {
		var out string
		if c.draft != "" {
			out, err = draftCommand(c.draft, c.config)
		} else {
			out, err = flowCommand(c.format, c.config)
		}
		if err != nil {
			var validationErr draftValidationError
			if errors.As(err, &validationErr) {
				fmt.Fprintln(stderr, "wtui:", err)
			} else {
				fmt.Fprintln(stderr, "wtui flow:", err)
			}
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

type draftValidationError string

func (err draftValidationError) Error() string { return string(err) }

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
			return "", draftValidationError(fmt.Sprintf(
				"the assistant could not produce a workflow that compiles after %d attempts; last validation error: %s",
				draftAttempts, ai.InertText(err.Error(), 300)))
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
