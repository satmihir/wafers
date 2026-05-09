package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/satmihir/wafers/internal/gitutil"
	"github.com/satmihir/wafers/internal/mount"
	"github.com/satmihir/wafers/internal/state"
)

const usage = `wafers creates cheap repo views backed by fuse-overlayfs.

Usage:
  wafers <command> [args]

Commands:
  add         Create a wafer and local branch
  git-commit  Commit the mounted wafer view onto its branch
  git-diff    Show committed wafer branch changes
  ls          List known wafers
  rm          Unmount and remove wafer state
  doctor      Check host support
  skill       Print an agent skill file

Other:
  help        Show command help
  version     Print version

Run "wafers help <command>" for details.
`

var Version = "dev"

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("missing command")
	}
	switch args[0] {
	case "add":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("add", stdout)
		}
		return runAdd(ctx, args[1:], stdout)
	case "git-commit":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("git-commit", stdout)
		}
		return runGitCommit(ctx, args[1:], stdout)
	case "git-diff":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("git-diff", stdout)
		}
		return runGitDiff(ctx, args[1:], stdout)
	case "ls":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("ls", stdout)
		}
		return runList(args[1:], stdout)
	case "rm":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("rm", stdout)
		}
		return runRemove(ctx, args[1:], stdout)
	case "doctor":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("doctor", stdout)
		}
		return runDoctor(ctx, args[1:], stdout)
	case "skill":
		if wantsCommandHelp(args[1:]) {
			return printCommandHelp("skill", stdout)
		}
		return runSkill(args[1:], stdout)
	case "help":
		if len(args) == 1 {
			fmt.Fprint(stdout, usage)
			return nil
		}
		if len(args) == 2 {
			return printCommandHelp(args[1], stdout)
		}
		return errors.New("usage: wafers help [command]")
	case "-h", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	case "-v", "--version", "version":
		if len(args) != 1 {
			return errors.New("usage: wafers version")
		}
		fmt.Fprintf(stdout, "wafers %s\n", Version)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func wantsCommandHelp(args []string) bool {
	return len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help")
}

func printCommandHelp(command string, stdout io.Writer) error {
	help, ok := commandHelp[command]
	if !ok {
		return fmt.Errorf("unknown command %q", command)
	}
	fmt.Fprint(stdout, help)
	return nil
}

var commandHelp = map[string]string{
	"add": `wafers add - create a wafer and local branch

Usage:
  wafers add <name> --at <mountpoint> --branch <branch> [--from <repo>] [--json]

Creates a fuse-overlayfs-backed repo view at <mountpoint>. The base repo is
used as the read-only lowerdir. wafers creates the requested local branch at
the base HEAD and records wafer metadata in the wafers state directory.

Required:
  <name>              Wafer name; use letters, numbers, dot, dash, underscore
  --at <mountpoint>   Empty directory where the wafer will be mounted
  --branch <branch>   New local branch to create for this wafer

Optional:
  --from <repo>       Base repo path; defaults to the current directory
  --json              Print machine-readable JSON output

Example:
  wafers add agent-1 --from /repo --at /tmp/agent-1 --branch agents/agent-1

Notes:
  - The base repo worktree must be clean, except for ignored files.
  - The branch must not already exist.
  - The mountpoint must be outside other Git repos.
  - .git is hidden inside the wafer view on purpose.
`,
	"git-commit": `wafers git-commit - commit the mounted wafer view

Usage:
  wafers git-commit <name> -m <message> [-- <paths>...]

Commits the entire mounted wafer view to the wafer branch. This is similar to
"git add -A && git commit", but wafers uses a private index and Git plumbing so
the base repo worktree and index are not modified.

Required:
  <name>              Wafer name
  -m, --message       Commit message

Optional:
  -- <paths>...       Commit only these paths, relative to the wafer root

Example:
  wafers git-commit agent-1 -m "fix parser"
  wafers git-commit agent-1 -m "fix parser" -- parser.go go.mod

Notes:
  - The wafer must be mounted.
  - Empty commits are refused.
  - Without paths, the entire wafer view is committed.
  - If the wafer branch moved outside wafers, the commit is refused.
`,
	"git-diff": `wafers git-diff - show committed wafer branch changes

Usage:
  wafers git-diff <name>

Shows the committed diff between the wafer base commit and the current wafer
branch tip. The wafer does not need to be mounted.

Required:
  <name>              Wafer name

Example:
  wafers git-diff agent-1

Notes:
  - This shows committed wafer branch changes only.
  - Uncommitted edits in the wafer mount are not included.
`,
	"ls": `wafers ls - list known wafers

Usage:
  wafers ls [--json]

Shows wafer name, branch, base commit, last commit, mount status, and
mountpoint.

Options:
  --json              Print machine-readable JSON output
`,
	"rm": `wafers rm - unmount and remove wafer state

Usage:
  wafers rm <name> [--force]

Unmounts the wafer if mounted, deletes wafer state, and keeps the local branch
and commits.

Required:
  <name>              Wafer name

Optional:
  --force             Remove even when the wafer upperdir has changes

Example:
  cd /tmp
  wafers rm agent-1 --force

Notes:
  - If removal says the device is busy, leave the mountpoint and retry.
`,
	"doctor": `wafers doctor - check host support

Usage:
  wafers doctor

Checks Linux, git, Git author identity, fuse-overlayfs, fusermount3, /dev/fuse,
and a small test mount. Missing author identity is reported as a warning because
the host can still run wafers, but git-commit will need user.name/user.email or
GIT_AUTHOR_NAME/GIT_AUTHOR_EMAIL.

Install hints:
  Debian/Ubuntu: sudo apt install fuse-overlayfs fuse3 git
  Fedora:        sudo dnf install fuse-overlayfs fuse3 git
  Arch:          sudo pacman -S fuse-overlayfs fuse3 git
`,
	"skill": `wafers skill - print an agent skill file

Usage:
  wafers skill

Prints Markdown instructions that teach an agent how to use wafers correctly.

Example:
  wafers skill > SKILL.md
`,
}

const doctorInstallHints = `
Install hints:
  Debian/Ubuntu: sudo apt install fuse-overlayfs fuse3 git
  Fedora:        sudo dnf install fuse-overlayfs fuse3 git
  Arch:          sudo pacman -S fuse-overlayfs fuse3 git

Containers must also expose /dev/fuse and allow FUSE mounts.
`

func runAdd(ctx context.Context, args []string, stdout io.Writer) error {
	parsed, err := parseAddArgs(args)
	if err != nil {
		return err
	}
	if runtime.GOOS != "linux" {
		return errors.New("wafers add requires Linux and fuse-overlayfs")
	}
	if parsed.At == "" || parsed.Branch == "" {
		return errors.New("add requires --at and --branch")
	}
	name := parsed.Name
	if err := state.ValidateName(name); err != nil {
		return err
	}
	if err := gitutil.ValidateBranch(ctx, parsed.Branch); err != nil {
		return err
	}

	store, err := state.OpenDefault()
	if err != nil {
		return err
	}
	unlock, err := store.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	if _, err := store.Load(name); err == nil {
		return fmt.Errorf("wafer %q already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	repo, err := gitutil.ResolveRepo(ctx, parsed.From)
	if err != nil {
		return err
	}
	clean, status, err := gitutil.WorktreeClean(ctx, repo.Root)
	if err != nil {
		return fmt.Errorf("check base repo worktree: %w", err)
	}
	if !clean {
		return dirtyBaseWorktreeError(status)
	}
	if gitutil.LocalBranchExists(ctx, repo.GitDir, parsed.Branch) {
		return fmt.Errorf("branch %q already exists", parsed.Branch)
	}
	mountpoint, err := filepath.Abs(parsed.At)
	if err != nil {
		return err
	}
	if err := mount.EnsureEmptyMountpoint(mountpoint); err != nil {
		return err
	}

	waferDir := store.WaferDir(name)
	meta := state.Meta{
		Version:    1,
		Name:       name,
		BaseRepo:   repo.Root,
		BaseGitDir: repo.GitDir,
		BaseCommit: repo.Head,
		Branch:     parsed.Branch,
		Mountpoint: mountpoint,
		Upperdir:   filepath.Join(waferDir, "upper"),
		Workdir:    filepath.Join(waferDir, "work"),
		Index:      filepath.Join(waferDir, "index"),
		LastCommit: repo.Head,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if err := store.InitWaferDirs(&meta); err != nil {
		return err
	}
	cleanup := true
	branchCreated := false
	defer func() {
		if cleanup {
			if branchCreated {
				_ = gitutil.DeleteLocalBranch(ctx, repo.GitDir, parsed.Branch)
			}
			_ = mount.Unmount(ctx, mountpoint)
			_ = os.RemoveAll(waferDir)
		}
	}()

	if err := mount.FuseOverlay(ctx, repo.Root, meta.Upperdir, meta.Workdir, mountpoint); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(mountpoint, ".git")); err != nil {
		return fmt.Errorf("hide .git: %w", err)
	}
	if gitutil.IsInsideWorkTree(ctx, mountpoint) {
		return errors.New("wafer mount still appears to be inside a Git worktree; choose a mountpoint outside any Git repository")
	}
	if err := gitutil.CreateLocalBranch(ctx, repo.GitDir, parsed.Branch, repo.Head); err != nil {
		return fmt.Errorf("create branch %q: %w", parsed.Branch, err)
	}
	branchCreated = true
	if err := store.Save(&meta); err != nil {
		return err
	}
	cleanup = false
	if parsed.JSON {
		out := addJSONOutput{
			Name:       meta.Name,
			Branch:     meta.Branch,
			BaseCommit: meta.BaseCommit,
			LastCommit: meta.LastCommit,
			Mountpoint: meta.Mountpoint,
			Status:     "mounted",
		}
		if err := writeJSON(stdout, out); err != nil {
			return err
		}
		return nil
	}
	fmt.Fprintf(stdout, "created wafer %q at %s on branch %s\n", name, mountpoint, parsed.Branch)
	return nil
}

func dirtyBaseWorktreeError(status string) error {
	msg := "base repo worktree is not clean; commit, stash, or remove changes before creating a wafer"
	if first := firstStatusEntry(status); first != "" {
		msg += "; first status entry: " + first
	}
	return errors.New(msg)
}

func firstStatusEntry(status string) string {
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimRight(line, "\r")
		}
	}
	return ""
}

func runList(args []string, stdout io.Writer) error {
	parsed, err := parseListArgs(args)
	if err != nil {
		return err
	}
	store, err := state.OpenDefault()
	if err != nil {
		return err
	}
	metas, bad, err := store.List()
	if err != nil {
		return err
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Name < metas[j].Name })
	sort.Strings(bad)
	if parsed.JSON {
		out := listJSONOutput{
			Wafers:         make([]listWaferJSONOutput, 0, len(metas)),
			InvalidEntries: bad,
		}
		for _, meta := range metas {
			status := "unmounted"
			if mount.IsMounted(meta.Mountpoint) {
				status = "mounted"
			}
			out.Wafers = append(out.Wafers, listWaferJSONOutput{
				Name:       meta.Name,
				Branch:     meta.Branch,
				BaseCommit: meta.BaseCommit,
				LastCommit: meta.LastCommit,
				Status:     status,
				Mountpoint: meta.Mountpoint,
			})
		}
		return writeJSON(stdout, out)
	}
	fmt.Fprintln(stdout, "NAME\tBRANCH\tBASE\tLAST\tSTATUS\tMOUNTPOINT")
	for _, meta := range metas {
		status := "unmounted"
		if mount.IsMounted(meta.Mountpoint) {
			status = "mounted"
		}
		base := meta.BaseCommit
		if len(base) > 12 {
			base = base[:12]
		}
		last := meta.LastCommit
		if last == "" {
			last = "-"
		} else if len(last) > 12 {
			last = last[:12]
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\t%s\n", meta.Name, meta.Branch, base, last, status, meta.Mountpoint)
	}
	for _, entry := range bad {
		fmt.Fprintf(stdout, "%s\t<invalid>\t-\t-\terror\t-\n", entry)
	}
	return nil
}

func runGitCommit(ctx context.Context, args []string, stdout io.Writer) error {
	parsed, err := parseGitCommitArgs(args)
	if err != nil {
		return err
	}
	if err := state.ValidateName(parsed.Name); err != nil {
		return err
	}

	store, err := state.OpenDefault()
	if err != nil {
		return err
	}
	unlock, err := store.Lock()
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := store.Load(parsed.Name)
	if err != nil {
		return err
	}
	if !mount.IsMounted(meta.Mountpoint) {
		return fmt.Errorf("wafer %q is not mounted at %s", meta.Name, meta.Mountpoint)
	}

	parent, err := gitutil.LocalBranchTip(ctx, meta.BaseGitDir, meta.Branch)
	if err != nil {
		return fmt.Errorf("read branch %q: %w", meta.Branch, err)
	}
	expected := meta.LastCommit
	if expected == "" {
		expected = meta.BaseCommit
	}
	if parent != expected {
		return fmt.Errorf("branch %q moved outside wafers; expected %s, found %s", meta.Branch, shortSHA(expected), shortSHA(parent))
	}
	if err := os.Remove(meta.Index); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reset wafer index: %w", err)
	}
	if err := gitutil.ReadTree(ctx, meta.BaseGitDir, meta.Index, parent); err != nil {
		return fmt.Errorf("seed wafer index: %w", err)
	}
	if err := addCommitPaths(ctx, parsed, meta); err != nil {
		return fmt.Errorf("update wafer index: %w", err)
	}
	tree, err := gitutil.WriteTree(ctx, meta.BaseGitDir, meta.Index)
	if err != nil {
		return fmt.Errorf("write wafer tree: %w", err)
	}
	parentTree, err := gitutil.TreeForCommit(ctx, meta.BaseGitDir, parent)
	if err != nil {
		return fmt.Errorf("read parent tree: %w", err)
	}
	if tree == parentTree {
		return errors.New("nothing to commit")
	}
	commit, err := gitutil.CommitTree(ctx, meta.BaseGitDir, tree, parent, parsed.Message)
	if err != nil {
		return fmt.Errorf("create commit: %w", err)
	}
	if err := gitutil.UpdateLocalBranch(ctx, meta.BaseGitDir, meta.Branch, commit, parent); err != nil {
		return fmt.Errorf("update branch %q: %w", meta.Branch, err)
	}
	meta.LastCommit = commit
	meta.UpdatedAt = time.Now()
	if err := store.Save(meta); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "committed wafer %q as %s\n", meta.Name, shortSHA(commit))
	return nil
}

func addCommitPaths(ctx context.Context, parsed gitCommitArgs, meta *state.Meta) error {
	if len(parsed.Paths) == 0 {
		return gitutil.AddAll(ctx, meta.BaseGitDir, meta.Index, meta.Mountpoint)
	}
	for _, path := range parsed.Paths {
		if err := validateCommitPath(path); err != nil {
			return err
		}
	}
	return gitutil.AddPaths(ctx, meta.BaseGitDir, meta.Index, meta.Mountpoint, parsed.Paths)
}

func validateCommitPath(path string) error {
	if path == "" {
		return errors.New("path must not be empty")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be relative to the wafer root", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("path %q must not contain parent traversal", path)
		}
	}
	return nil
}

func runGitDiff(ctx context.Context, args []string, stdout io.Writer) error {
	name, err := parseNameOnlyArgs("git-diff", args)
	if err != nil {
		return err
	}
	if err := state.ValidateName(name); err != nil {
		return err
	}
	store, err := state.OpenDefault()
	if err != nil {
		return err
	}
	meta, err := store.Load(name)
	if err != nil {
		return err
	}
	diff, err := gitutil.Diff(ctx, meta.BaseRepo, meta.BaseCommit, meta.Branch)
	if err != nil {
		return fmt.Errorf("diff wafer %q: %w", meta.Name, err)
	}
	fmt.Fprint(stdout, diff)
	return nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func runRemove(ctx context.Context, args []string, stdout io.Writer) error {
	parsed, err := parseRemoveArgs(args)
	if err != nil {
		return err
	}
	name := parsed.Name
	if err := state.ValidateName(name); err != nil {
		return err
	}
	store, err := state.OpenDefault()
	if err != nil {
		return err
	}
	unlock, err := store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	meta, err := store.Load(name)
	if err != nil {
		return err
	}
	if changed, err := state.UpperHasUserChanges(meta.Upperdir); err != nil {
		return err
	} else if changed && !parsed.Force {
		return fmt.Errorf("wafer %q has changes; rerun with --force to delete it", name)
	}
	if mount.IsMounted(meta.Mountpoint) {
		if err := mount.Unmount(ctx, meta.Mountpoint); err != nil {
			return err
		}
	}
	if err := store.Remove(name); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed wafer %q\n", name)
	return nil
}

func runDoctor(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: wafers doctor")
	}
	var failures []string
	var warnings []string
	check := func(label string, err error) {
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			fmt.Fprintf(stdout, "FAIL %s: %v\n", label, err)
			return
		}
		fmt.Fprintf(stdout, "OK   %s\n", label)
	}
	warn := func(label, hint string, err error) {
		if err == nil {
			fmt.Fprintf(stdout, "OK   %s\n", label)
			return
		}
		warnings = append(warnings, fmt.Sprintf("%s: %v", label, err))
		fmt.Fprintf(stdout, "WARN %s: %v\n", label, err)
		if hint != "" {
			fmt.Fprintf(stdout, "     %s\n", hint)
		}
	}
	check("linux", requireLinux())
	gitErr := mount.LookPath("git")
	check("git", gitErr)
	if gitErr == nil {
		_, err := gitutil.AuthorIdentity(ctx, ".")
		warn("git author identity", "set user.name/user.email with git config or export GIT_AUTHOR_NAME/GIT_AUTHOR_EMAIL", err)
	}
	check("fuse-overlayfs", mount.LookPath("fuse-overlayfs"))
	check("fusermount3", mount.LookPath("fusermount3"))
	check("/dev/fuse", mount.CheckFuseDevice())
	check("test mount", mount.TestMount(ctx))
	if len(failures) > 0 {
		fmt.Fprint(stdout, doctorInstallHints)
		return fmt.Errorf("doctor found %d problem(s): %s", len(failures), strings.Join(failures, "; "))
	}
	if len(warnings) > 0 {
		fmt.Fprintf(stdout, "doctor completed with %d warning(s)\n", len(warnings))
	}
	return nil
}

func runSkill(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: wafers skill")
	}
	fmt.Fprint(stdout, skillMarkdown)
	return nil
}

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("requires Linux, got %s", runtime.GOOS)
	}
	return nil
}

type addArgs struct {
	Name   string
	At     string
	Branch string
	From   string
	JSON   bool
}

func parseAddArgs(args []string) (addArgs, error) {
	parsed := addArgs{From: "."}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--at" || arg == "--branch" || arg == "--from":
			if i+1 >= len(args) {
				return addArgs{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			setAddFlag(&parsed, arg, args[i])
		case arg == "--json":
			parsed.JSON = true
		case strings.HasPrefix(arg, "--at="):
			parsed.At = strings.TrimPrefix(arg, "--at=")
		case strings.HasPrefix(arg, "--branch="):
			parsed.Branch = strings.TrimPrefix(arg, "--branch=")
		case strings.HasPrefix(arg, "--from="):
			parsed.From = strings.TrimPrefix(arg, "--from=")
		case strings.HasPrefix(arg, "-"):
			return addArgs{}, fmt.Errorf("unknown add flag %q", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return addArgs{}, errors.New("usage: wafers add <name> --at <mountpoint> --branch <branch> [--from <repo>]")
	}
	parsed.Name = positional[0]
	return parsed, nil
}

func setAddFlag(parsed *addArgs, flag, value string) {
	switch flag {
	case "--at":
		parsed.At = value
	case "--branch":
		parsed.Branch = value
	case "--from":
		parsed.From = value
	}
}

type removeArgs struct {
	Name  string
	Force bool
}

type listArgs struct {
	JSON bool
}

type gitCommitArgs struct {
	Name    string
	Message string
	Paths   []string
}

func parseGitCommitArgs(args []string) (gitCommitArgs, error) {
	var parsed gitCommitArgs
	var positional []string
	messageSet := false
	pathsMode := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if pathsMode {
			parsed.Paths = append(parsed.Paths, arg)
			continue
		}
		switch {
		case arg == "--":
			pathsMode = true
		case arg == "-m" || arg == "--message":
			if messageSet {
				return gitCommitArgs{}, errors.New("git-commit accepts exactly one -m/--message value")
			}
			if i+1 >= len(args) {
				return gitCommitArgs{}, fmt.Errorf("%s requires a value", arg)
			}
			i++
			parsed.Message = args[i]
			messageSet = true
		case strings.HasPrefix(arg, "-m="):
			if messageSet {
				return gitCommitArgs{}, errors.New("git-commit accepts exactly one -m/--message value")
			}
			parsed.Message = strings.TrimPrefix(arg, "-m=")
			messageSet = true
		case strings.HasPrefix(arg, "--message="):
			if messageSet {
				return gitCommitArgs{}, errors.New("git-commit accepts exactly one -m/--message value")
			}
			parsed.Message = strings.TrimPrefix(arg, "--message=")
			messageSet = true
		case strings.HasPrefix(arg, "-"):
			return gitCommitArgs{}, fmt.Errorf("unknown git-commit flag %q", arg)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 || !messageSet {
		return gitCommitArgs{}, errors.New("usage: wafers git-commit <name> -m <message> [-- <paths>...]")
	}
	if pathsMode && len(parsed.Paths) == 0 {
		return gitCommitArgs{}, errors.New("git-commit requires at least one path after --")
	}
	parsed.Name = positional[0]
	return parsed, nil
}

func parseRemoveArgs(args []string) (removeArgs, error) {
	var parsed removeArgs
	var positional []string
	for _, arg := range args {
		switch arg {
		case "--force":
			parsed.Force = true
		default:
			if strings.HasPrefix(arg, "-") {
				return removeArgs{}, fmt.Errorf("unknown rm flag %q", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return removeArgs{}, errors.New("usage: wafers rm <name> [--force]")
	}
	parsed.Name = positional[0]
	return parsed, nil
}

func parseListArgs(args []string) (listArgs, error) {
	var parsed listArgs
	for _, arg := range args {
		switch arg {
		case "--json":
			parsed.JSON = true
		default:
			return listArgs{}, errors.New("usage: wafers ls [--json]")
		}
	}
	return parsed, nil
}

func parseNameOnlyArgs(command string, args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: wafers %s <name>", command)
	}
	return args[0], nil
}

type addJSONOutput struct {
	Name       string `json:"name"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"base_commit"`
	LastCommit string `json:"last_commit"`
	Mountpoint string `json:"mountpoint"`
	Status     string `json:"status"`
}

type listWaferJSONOutput struct {
	Name       string `json:"name"`
	Branch     string `json:"branch"`
	BaseCommit string `json:"base_commit"`
	LastCommit string `json:"last_commit"`
	Status     string `json:"status"`
	Mountpoint string `json:"mountpoint"`
}

type listJSONOutput struct {
	Wafers         []listWaferJSONOutput `json:"wafers"`
	InvalidEntries []string              `json:"invalid_entries"`
}

func writeJSON(stdout io.Writer, v any) error {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
