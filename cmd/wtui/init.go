package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/D1ssolve/wtui/internal/app"
	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/gitflow"
)

const configHeader = "# wtui configuration. Every key is described in https://github.com/D1ssolve/wtui/blob/main/docs/configuration.md\n"

// maxAnswer bounds one line of input so a runaway pipe fails instead of growing memory.
const maxAnswer = 64 * 1024

const promptAttempts = 3

// clock names backups; tests pin it to force same-second collisions.
var clock = time.Now

// linkFile and replaceFile are the publication primitives; tests make them fail like exFAT does.
var linkFile, replaceFile = os.Link, os.Rename
var acquireConfigLock = lockPublication

// draftFunc turns a one-line description into a validated `workflow:` document (draftWorkflow output).
type draftFunc func(ctx context.Context, description string) (string, error)

// draftWithAssistant uses the default AI provider: there is no config yet to choose another.
func draftWithAssistant(ctx context.Context, description string) (string, error) {
	assistant, err := app.BuildAssistant(nil)
	if err != nil {
		return "", err
	}
	return draftWorkflow(ctx, assistant, description)
}

// initCommand writes a first config from flags and, unless --yes, answers read from stdin.
// It returns the process exit code.
func initCommand(c cliArgs, stdin io.Reader, stdout, stderr io.Writer, draft draftFunc) int {
	badFlag := func(err error) int {
		fmt.Fprintf(stderr, "wtui init: %v\n%s", err, usageText)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(stderr, "wtui: %v\n", err)
		return 1
	}

	target := c.config
	if target == "" {
		var err error
		if target, err = config.DefaultPath(); err != nil {
			return fail(err)
		}
	}
	target, err := absPath(target)
	if err != nil {
		return fail(err)
	}
	if err := checkTarget(target, c.force); err != nil {
		return fail(err)
	}

	root := c.root
	if root == "" {
		if root, err = os.Getwd(); err != nil {
			return fail(err)
		}
	}
	if root, err = existingDir(root); err != nil {
		return badFlag(fmt.Errorf("--root: %w", err))
	}
	tasksRoot := ""
	if c.tasksRoot != "" {
		if tasksRoot, err = absPath(c.tasksRoot); err != nil {
			return badFlag(fmt.Errorf("--tasks-root: %w", err))
		}
	}
	editor := c.editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "code"
	}
	preset := c.preset
	if preset == "" {
		preset = "git-flow"
	}
	workflow := presetNode(preset)

	if !c.yes {
		in := bufio.NewScanner(stdin)
		in.Buffer(make([]byte, 0, 4096), maxAnswer)
		p := &prompter{in: in, out: stdout}
		if root, err = p.askDir("Directory with your service repositories (root_dir)", root); err != nil {
			return fail(err)
		}
		if tasksRoot == "" {
			tasksRoot = filepath.Join(root, ".tasks")
		}
		answer, err := p.ask("Directory for task worktrees (tasks_root)", tasksRoot)
		if err != nil {
			return fail(err)
		}
		if tasksRoot, err = absPath(answer); err != nil {
			return fail(err)
		}
		if editor, err = p.ask("Editor command (editor)", editor); err != nil {
			return fail(err)
		}
		if workflow, err = p.chooseWorkflow(preset, draft); err != nil {
			return fail(err)
		}
	}
	if tasksRoot == "" {
		tasksRoot = filepath.Join(root, ".tasks")
	}

	data, err := renderConfig(root, tasksRoot, editor, workflow)
	if err != nil {
		return fail(err)
	}
	backup, err := writeConfig(target, data, configPublication{force: c.force})
	if err != nil {
		return fail(err)
	}
	if backup != "" {
		fmt.Fprintf(stdout, "Backed up the previous config to %s\n", backup)
	}

	start := "wtui"
	if c.config != "" {
		start = "wtui --config " + shellArg(target)
	}
	width := max(len(start+" flow"), len("wtui --help"))
	fmt.Fprintf(stdout, "Wrote %s\nNext steps:\n  %-*s  start wtui\n  %-*s  see the workflow\n  %-*s  all commands\n",
		target, width, start, width, start+" flow", width, "wtui --help")
	return 0
}

// shellArg quotes s for the shell the printed command is meant for: POSIX sh on Unix (single quotes,
// nothing expands), cmd.exe on Windows (double quotes; Windows paths cannot contain them).
// PowerShell users on Windows must adapt the line.
func shellArg(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+:@%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

type prompter struct {
	in  *bufio.Scanner
	out io.Writer
}

// ask prints the question and returns the trimmed answer; an empty line or end of input
// yields def, and end of input without a default is an error.
func (p *prompter) ask(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(p.out, "%s: ", question)
	}
	if !p.in.Scan() {
		fmt.Fprintln(p.out)
		if err := p.in.Err(); errors.Is(err, bufio.ErrTooLong) {
			return "", fmt.Errorf("input line is longer than %d bytes", maxAnswer)
		} else if err != nil {
			return "", fmt.Errorf("read answer: %w", err)
		}
		if def == "" {
			return "", fmt.Errorf("input ended before %q was answered; nothing was written", question)
		}
		return def, nil
	}
	answer := strings.TrimSpace(p.in.Text())
	if !utf8.ValidString(answer) || strings.ContainsFunc(answer, unicode.IsControl) {
		return "", fmt.Errorf("the answer to %q contains control characters or invalid UTF-8; nothing was written", question)
	}
	if answer == "" {
		return def, nil
	}
	return answer, nil
}

func (p *prompter) askDir(question, def string) (string, error) {
	for range promptAttempts {
		answer, err := p.ask(question, def)
		if err != nil {
			return "", err
		}
		dir, err := existingDir(answer)
		if err == nil {
			return dir, nil
		}
		fmt.Fprintf(p.out, "  %v\n", err)
	}
	return "", fmt.Errorf("no existing directory given after %d attempts; nothing was written", promptAttempts)
}

func (p *prompter) chooseWorkflow(preset string, draft draftFunc) (*yaml.Node, error) {
	names := gitflow.PresetNames()
	aiChoice := len(names) + 1
	for invalid := 0; ; {
		fmt.Fprintln(p.out, "Workflow:")
		for i, name := range names {
			fmt.Fprintf(p.out, "  %d) %s\n", i+1, name)
		}
		fmt.Fprintf(p.out, "  %d) describe your process (AI draft)\n", aiChoice)
		answer, err := p.ask("Choose", strconv.Itoa(slices.Index(names, preset)+1))
		if err != nil {
			return nil, err
		}
		n, _ := strconv.Atoi(answer)
		switch {
		case n >= 1 && n <= len(names):
			return presetNode(names[n-1]), nil
		case slices.Contains(names, answer):
			return presetNode(answer), nil
		case n == aiChoice:
			description, err := p.ask("Describe how your team works in one line", "")
			if err != nil {
				return nil, err
			}
			if description == "" {
				fmt.Fprintln(p.out, "  No description given; choose again.")
				continue
			}
			fmt.Fprintln(p.out, "Drafting a workflow with AI...")
			node, text, err := draftNode(draft, description)
			if err != nil {
				fmt.Fprintf(p.out, "  AI draft failed: %v\n  Choose a preset or try again.\n", err)
				continue
			}
			fmt.Fprint(p.out, text)
			return node, nil
		default:
			if invalid++; invalid == promptAttempts {
				return nil, fmt.Errorf("no valid workflow choice after %d attempts; nothing was written", promptAttempts)
			}
			fmt.Fprintf(p.out, "  Enter a number from 1 to %d.\n", aiChoice)
		}
	}
}

func draftNode(draft draftFunc, description string) (*yaml.Node, string, error) {
	text, err := draft(context.Background(), description)
	if err != nil {
		return nil, "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return nil, "", fmt.Errorf("parse draft: %w", err)
	}
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		m := doc.Content[0].Content
		for i := 0; i+1 < len(m); i += 2 {
			if m[i].Value == "workflow" {
				return m[i+1], text, nil
			}
		}
	}
	return nil, "", errors.New("the draft has no workflow block")
}

func presetNode(name string) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "extends"},
		{Kind: yaml.ScalarNode, Value: name},
	}}
}

// renderConfig marshals the answers so any path or command text round-trips through YAML.
func renderConfig(root, tasksRoot, editor string, workflow *yaml.Node) ([]byte, error) {
	b := bytes.NewBufferString(configHeader)
	enc := yaml.NewEncoder(b)
	enc.SetIndent(2)
	err := enc.Encode(struct {
		RootDir   string     `yaml:"root_dir"`
		TasksRoot string     `yaml:"tasks_root"`
		Editor    string     `yaml:"editor"`
		Workflow  *yaml.Node `yaml:"workflow"`
	}{root, tasksRoot, editor, workflow})
	if err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return b.Bytes(), nil
}

// writeConfig validates the exact bytes through the real loader from a temp file next to the
// target, then publishes that same file. It writes nothing to stdout: output starts after the commit.
func writeConfig(target string, data []byte, publication configPublication) (string, error) {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".wtui-init-*.yaml")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed; after a link the temp name is just a second name
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := validateConfigFile(tmp.Name(), publication.expected != nil && *publication.expected == nil); err != nil {
		return "", fmt.Errorf("the generated config is invalid, nothing was written: %w", err)
	}
	return publish(tmp.Name(), target, publication)
}

type configPublication struct {
	force    bool
	expected *[]byte // nil is init's unconditional policy; a pointer to a nil slice expects no file.
}

// publish runs under a lock every wtui config publisher on this target shares, so concurrent writers take
// turns and each backup holds exactly the bytes its writer replaced. An existing config never
// leaves its path: its bytes are first copied to a private synced backup, then the validated temp
// file is renamed over it in one step. A new config is hard-linked, which cannot clobber a file
// another program created meanwhile. Returns the backup path, if any.
func publish(tmp, target string, publication configPublication) (string, error) {
	unlock, err := acquireConfigLock(lockPath(target))
	if err != nil {
		return "", fmt.Errorf("lock %s: %w", target, err)
	}
	defer unlock()
	info, err := inspectTarget(target, publication.force || publication.expected != nil)
	if err != nil {
		return "", err
	}
	if publication.expected != nil {
		want := *publication.expected
		if (info == nil) != (want == nil) {
			return "", staleConfig(target)
		}
		if info != nil {
			got, err := os.ReadFile(target)
			if errors.Is(err, os.ErrNotExist) || err == nil && !bytes.Equal(got, want) {
				return "", staleConfig(target)
			}
			if err != nil {
				return "", err
			}
		}
	}
	if info == nil {
		if err := os.Chmod(tmp, 0o644); err != nil {
			return "", err
		}
		// ponytail: creating needs hard links (not FAT/exFAT); there init fails and writes nothing.
		if err := linkFile(tmp, target); errors.Is(err, os.ErrExist) {
			if publication.expected != nil {
				return "", staleConfig(target)
			}
			return "", fmt.Errorf("%s was created by another program while init was writing; nothing was replaced, run init again", target)
		} else if err != nil {
			return "", err
		}
		return "", nil
	}
	backup, err := copyToBackup(target)
	if err != nil {
		return "", fmt.Errorf("back up %s: %w", target, err)
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		os.Remove(backup)
		return "", err
	}
	if err := replaceFile(tmp, target); err != nil {
		os.Remove(backup)
		return "", err
	}
	return backup, nil
}

func staleConfig(target string) error {
	return fmt.Errorf("%s changed since it was loaded; nothing was replaced, reload the config and try again", target)
}

// lockPath is a hidden sibling that stays after use: deleting a lock others may hold reopens the race.
func lockPath(target string) string {
	return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".lock")
}

func copyToBackup(target string) (string, error) {
	src, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer src.Close()
	dst, err := reserveBackup(target)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(dst, src)
	if err == nil {
		err = dst.Sync()
	}
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(dst.Name())
		return "", err
	}
	return dst.Name(), nil
}

// checkTarget refuses links and non-regular files whatever force says, and existing files without force.
func checkTarget(target string, force bool) error {
	_, err := inspectTarget(target, force)
	return err
}

func inspectTarget(target string, force bool) (os.FileInfo, error) {
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	case info.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("%s is a symbolic link; wtui does not replace links: edit the file it points to, or remove the link and try again", target)
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("%s exists and is not a regular file; move it away and try again", target)
	case !force:
		return nil, fmt.Errorf("%s already exists; use --force to replace it", target)
	}
	return info, nil
}

// reserveBackup creates a private, never-reused backup file; a same-second name gets a -N suffix.
func reserveBackup(target string) (*os.File, error) {
	base := target + ".bak-" + clock().Format("20060102-150405")
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if !errors.Is(err, os.ErrExist) {
			return f, err
		}
	}
}

func validateConfigFile(path string, requireRoot bool) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	if cfg, err = cfg.Effective(); err != nil {
		return err
	}
	if requireRoot {
		if info, err := os.Stat(cfg.RootDir); err != nil || !info.IsDir() {
			return errors.New("there is no usable root_dir; run wtui init with an existing root directory first")
		}
	}
	_, err = gitflow.Resolve(cfg)
	return err
}

func absPath(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	return filepath.Abs(p)
}

func existingDir(p string) (string, error) {
	dir, err := absPath(p)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not an existing directory", dir)
	}
	return dir, nil
}
