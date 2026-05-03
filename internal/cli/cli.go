package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mihirsathe/wafers/internal/gitutil"
	"github.com/mihirsathe/wafers/internal/mount"
	"github.com/mihirsathe/wafers/internal/state"
)

const usage = `wafers creates cheap repo views backed by fuse-overlayfs.

Usage:
  wafers add <name> --at <mountpoint> --branch <branch> [--from <repo>]
  wafers git-commit <name> -m <message>
  wafers ls
  wafers rm <name> [--force]
  wafers doctor
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("missing command")
	}
	switch args[0] {
	case "add":
		return runAdd(ctx, args[1:], stdout)
	case "git-commit":
		return runGitCommit(ctx, args[1:], stdout)
	case "ls":
		return runList(args[1:], stdout)
	case "rm":
		return runRemove(ctx, args[1:], stdout)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

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
	fmt.Fprintf(stdout, "created wafer %q at %s on branch %s\n", name, mountpoint, parsed.Branch)
	return nil
}

func runList(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return errors.New("usage: wafers ls")
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
	fmt.Fprintln(stdout, "NAME\tBRANCH\tBASE\tSTATUS\tMOUNTPOINT")
	for _, meta := range metas {
		status := "unmounted"
		if mount.IsMounted(meta.Mountpoint) {
			status = "mounted"
		}
		base := meta.BaseCommit
		if len(base) > 12 {
			base = base[:12]
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", meta.Name, meta.Branch, base, status, meta.Mountpoint)
	}
	sort.Strings(bad)
	for _, entry := range bad {
		fmt.Fprintf(stdout, "%s\t<invalid>\t-\terror\t-\n", entry)
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
	if err := gitutil.AddAll(ctx, meta.BaseGitDir, meta.Index, meta.Mountpoint); err != nil {
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
	check := func(label string, err error) {
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", label, err))
			fmt.Fprintf(stdout, "FAIL %s: %v\n", label, err)
			return
		}
		fmt.Fprintf(stdout, "OK   %s\n", label)
	}
	check("linux", requireLinux())
	check("git", mount.LookPath("git"))
	check("fuse-overlayfs", mount.LookPath("fuse-overlayfs"))
	check("fusermount3", mount.LookPath("fusermount3"))
	check("/dev/fuse", mount.CheckFuseDevice())
	check("test mount", mount.TestMount(ctx))
	if len(failures) > 0 {
		return fmt.Errorf("doctor found %d problem(s): %s", len(failures), strings.Join(failures, "; "))
	}
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

type gitCommitArgs struct {
	Name    string
	Message string
}

func parseGitCommitArgs(args []string) (gitCommitArgs, error) {
	var parsed gitCommitArgs
	var positional []string
	messageSet := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
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
		return gitCommitArgs{}, errors.New("usage: wafers git-commit <name> -m <message>")
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
