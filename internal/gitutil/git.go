package gitutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct {
	Root   string
	GitDir string
	Head   string
}

func ResolveRepo(ctx context.Context, dir string) (Repo, error) {
	root, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, fmt.Errorf("%s is not inside a Git worktree: %w", dir, err)
	}
	gitDir, err := gitOutput(ctx, root, "rev-parse", "--git-dir")
	if err != nil {
		return Repo{}, err
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	head, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Repo{}, err
	}
	return Repo{Root: root, GitDir: filepath.Clean(gitDir), Head: head}, nil
}

func ValidateBranch(ctx context.Context, branch string) error {
	if branch == "" {
		return fmt.Errorf("branch must not be empty")
	}
	if _, err := gitOutput(ctx, ".", "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("invalid branch %q: %w", branch, err)
	}
	return nil
}

func AuthorIdentity(ctx context.Context, dir string) (string, error) {
	return gitOutput(ctx, dir, "var", "GIT_AUTHOR_IDENT")
}

func WorktreeStatus(ctx context.Context, root string) (string, error) {
	return gitOutputPreserveStatus(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal", "--ignored=no")
}

func WorktreeClean(ctx context.Context, root string) (bool, string, error) {
	status, err := WorktreeStatus(ctx, root)
	if err != nil {
		return false, "", err
	}
	return status == "", status, nil
}

func IsInsideWorkTree(ctx context.Context, dir string) bool {
	out, err := gitOutput(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func IsAncestor(ctx context.Context, gitDir, base, tip string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "merge-base", "--is-ancestor", base, tip)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		return true, nil
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return false, fmt.Errorf("%s", msg)
	}
}

func ReadTree(ctx context.Context, gitDir, indexPath, treeish string) error {
	_, err := gitWithIndex(ctx, "", gitDir, indexPath, "read-tree", treeish)
	return err
}

func AddAll(ctx context.Context, gitDir, indexPath, workTree string) error {
	_, err := gitWithIndex(ctx, workTree, gitDir, indexPath, "--work-tree", workTree, "add", "-A", "--", ".")
	return err
}

func AddPaths(ctx context.Context, gitDir, indexPath, workTree string, paths []string) error {
	args := append([]string{"--work-tree", workTree, "add", "-A", "--"}, paths...)
	_, err := gitWithIndex(ctx, workTree, gitDir, indexPath, args...)
	return err
}

func WriteTree(ctx context.Context, gitDir, indexPath string) (string, error) {
	return gitWithIndex(ctx, "", gitDir, indexPath, "write-tree")
}

func TreeForWorktree(ctx context.Context, gitDir, indexPath, workTree, base string) (string, error) {
	if err := ReadTree(ctx, gitDir, indexPath, base); err != nil {
		return "", err
	}
	if err := AddAll(ctx, gitDir, indexPath, workTree); err != nil {
		return "", err
	}
	return WriteTree(ctx, gitDir, indexPath)
}

func TreeForCommit(ctx context.Context, gitDir, commit string) (string, error) {
	return gitWithDir(ctx, "", gitDir, "rev-parse", commit+"^{tree}")
}

func CommitTree(ctx context.Context, gitDir, tree, parent, message string) (string, error) {
	return gitWithDir(ctx, "", gitDir, "commit-tree", tree, "-p", parent, "-m", message)
}

func LocalBranchRef(branch string) string {
	return "refs/heads/" + branch
}

func LocalBranchExists(ctx context.Context, gitDir, branch string) bool {
	_, err := gitWithDir(ctx, "", gitDir, "show-ref", "--verify", "--quiet", LocalBranchRef(branch))
	return err == nil
}

func CreateLocalBranch(ctx context.Context, gitDir, branch, commit string) error {
	_, err := gitWithDir(ctx, "", gitDir, "update-ref", LocalBranchRef(branch), commit, "")
	return err
}

func DeleteLocalBranch(ctx context.Context, gitDir, branch string) error {
	_, err := gitWithDir(ctx, "", gitDir, "update-ref", "-d", LocalBranchRef(branch))
	return err
}

func LocalBranchTip(ctx context.Context, gitDir, branch string) (string, error) {
	return gitWithDir(ctx, "", gitDir, "rev-parse", "--verify", LocalBranchRef(branch))
}

func UpdateLocalBranch(ctx context.Context, gitDir, branch, newCommit, oldCommit string) error {
	_, err := gitWithDir(ctx, "", gitDir, "update-ref", LocalBranchRef(branch), newCommit, oldCommit)
	return err
}

func CommitParent(ctx context.Context, gitDir, commit string) (string, error) {
	return gitWithDir(ctx, "", gitDir, "rev-parse", commit+"^")
}

func ShowFile(ctx context.Context, gitDir, commit, path string) (string, error) {
	return gitWithDir(ctx, "", gitDir, "show", commit+":"+path)
}

func PathExistsInCommit(ctx context.Context, gitDir, commit, path string) bool {
	_, err := gitWithDir(ctx, "", gitDir, "cat-file", "-e", commit+":"+path)
	return err == nil
}

func Diff(ctx context.Context, repoRoot, base, branch string) (string, error) {
	return gitOutputPreserveRaw(ctx, repoRoot, "diff", base+"..."+branch)
}

func DiffBinary(ctx context.Context, repoRoot, base, tip string) (string, error) {
	return gitOutputPreserveRaw(ctx, repoRoot, "diff", "--binary", base+".."+tip)
}

func ApplyPatch(ctx context.Context, workTree, patch string) error {
	if patch == "" {
		return nil
	}
	_, err := runGitInput(ctx, workTree, nil, patch, "apply", "--binary")
	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return runGit(ctx, dir, nil, args...)
}

func gitOutputPreserveStatus(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := runGitCommand(ctx, dir, nil, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}

func gitOutputPreserveRaw(ctx context.Context, dir string, args ...string) (string, error) {
	return runGitCommand(ctx, dir, nil, args...)
}

func gitWithDir(ctx context.Context, dir, gitDir string, args ...string) (string, error) {
	fullArgs := append([]string{"--git-dir", gitDir}, args...)
	return runGit(ctx, dir, nil, fullArgs...)
}

func gitWithIndex(ctx context.Context, dir, gitDir, indexPath string, args ...string) (string, error) {
	fullArgs := append([]string{"--git-dir", gitDir}, args...)
	env := append(os.Environ(), "GIT_INDEX_FILE="+indexPath)
	return runGit(ctx, dir, env, fullArgs...)
}

func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	out, err := runGitCommand(ctx, dir, env, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runGitCommand(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return string(out), nil
}

func runGitInput(ctx context.Context, dir string, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return string(out), nil
}
