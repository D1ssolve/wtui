//go:build integration

package app

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/config"
	"github.com/D1ssolve/wtui/internal/task"
)

func TestConfiguredRemoteImport(t *testing.T) {
	for _, mode := range []string{"only-upstream", "fork-and-upstream", "omitted", "unconfigured"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			repo := filepath.Join(root, "svc")
			tasks := t.TempDir()
			bin := t.TempDir()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("WTUI_ROOT", root)
			t.Setenv("WTUI_TASKS_ROOT", tasks)
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_ALLOW_PROTOCOL", "file")
			git := func(dir string, args ...string) string {
				t.Helper()
				cmd := exec.CommandContext(t.Context(), "git", args...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			for _, name := range []string{"gh", "glab"} {
				script := "#!/bin/sh\nexit 1\n"
				if name == "gh" {
					script = "#!/bin/sh\n[ \"$1\" = --version ] && exit 0\ncase \"$*\" in *\"$EXPECTED_REPO\"*) printf '%s\\n' '[{\"number\":17,\"title\":\"selected\"}]';; *) exit 2;; esac\n"
				}
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			git(root, "init", "-b", "main", repo)
			git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fork tip")
			forkTip := git(repo, "rev-parse", "HEAD")
			fork := filepath.Join(t.TempDir(), "fork.git")
			git(repo, "init", "--bare", fork)
			const branch = "feature/APP-7"
			ref := "refs/heads/" + branch
			git(repo, "push", fork, "HEAD:"+ref)
			git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "upstream tip")
			tip := git(repo, "rev-parse", "HEAD")
			selected := "upstream"
			setting := "forge: {remote: upstream}"
			if mode == "omitted" {
				selected = "origin"
				setting = ""
			}
			bare := filepath.Join(t.TempDir(), "service.git")
			if mode == "unconfigured" {
				bare = filepath.Join(repo, selected)
			}
			git(repo, "init", "--bare", bare)
			git(repo, "push", bare, "HEAD:"+ref)
			url := "file://github.com" + filepath.ToSlash(bare)
			t.Setenv("EXPECTED_REPO", "github.com"+strings.TrimSuffix(filepath.ToSlash(bare), ".git"))
			if mode != "unconfigured" {
				git(repo, "remote", "add", selected, url)
			}
			if mode == "fork-and-upstream" || mode == "unconfigured" {
				git(repo, "remote", "add", "origin", fork)
				git(repo, "fetch", "origin")
			}
			cfgPath := filepath.Join(root, "config.yaml")
			if err := os.WriteFile(cfgPath, []byte(setting), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = cfg.Effective()
			if err != nil {
				t.Fatal(err)
			}
			deps, err := BuildDependencies(cfg, slog.Default())
			if err != nil {
				t.Fatal(err)
			}
			_, err = deps.Manager.Init(t.Context(), task.InitParams{TaskID: "APP-7", FromRemoteBranch: true})
			if mode == "unconfigured" {
				if _, localErr := deps.Manager.Init(t.Context(), task.InitParams{TaskID: "APP-8", Services: []string{"svc"}, BaseBranch: "main"}); localErr == nil || !strings.Contains(localErr.Error(), "forge.remote") {
					t.Errorf("ordinary init missing remote: %v", localErr)
				}
				if err == nil || !strings.Contains(err.Error(), "forge.remote") || !strings.Contains(err.Error(), selected) {
					t.Fatalf("missing named remote: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(tasks, "APP-7", "svc")
			if got := git(tree, "rev-parse", "HEAD"); got != tip {
				t.Fatalf("import tip %s, want %s (fork %s)", got, tip, forkTip)
			}
			if got := git(tree, "rev-parse", "--abbrev-ref", "@{u}"); got != selected+"/"+branch {
				t.Fatalf("tracking %s", got)
			}
			svcs, err := deps.Manager.ListServices(t.Context(), "APP-7")
			if err != nil || len(svcs) != 1 || svcs[0].RemoteURL != url {
				t.Fatalf("services %+v: %v", svcs, err)
			}
			mrs, err := deps.Manager.ForgeCreateMissingMRs(t.Context(), "APP-7", "review", true)
			if err != nil || len(mrs.Services) != 1 || mrs.Services[0].MR.Number != 17 {
				t.Fatalf("fake forge: %+v %v", mrs, err)
			}
			git(tree, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "task change")
			head := git(tree, "rev-parse", "HEAD")
			if err := deps.Manager.PushService(t.Context(), "APP-7", "svc", nil); err != nil {
				t.Fatal(err)
			}
			if got := git(bare, "rev-parse", ref); got != head {
				t.Fatal("selected remote not updated")
			}
			if got := git(fork, "rev-parse", ref); got != forkTip {
				t.Fatal("fork moved")
			}
			t.Logf("%s imported=%s published=%s fork=%s", selected, tip, head, forkTip)
		})
	}
}
