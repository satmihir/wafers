package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

type Store struct {
	Root string
}

type Meta struct {
	Version    int       `json:"version"`
	Name       string    `json:"name"`
	BaseRepo   string    `json:"base_repo"`
	BaseGitDir string    `json:"base_git_dir"`
	BaseCommit string    `json:"base_commit"`
	Branch     string    `json:"branch"`
	Mountpoint string    `json:"mountpoint"`
	Upperdir   string    `json:"upperdir"`
	Workdir    string    `json:"workdir"`
	Index      string    `json:"index"`
	LastCommit string    `json:"last_commit"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidateName(name string) error {
	if name == "" {
		return errors.New("wafer name must not be empty")
	}
	if name == "." || name == ".." || !namePattern.MatchString(name) {
		return fmt.Errorf("invalid wafer name %q; use only A-Z, a-z, 0-9, dot, underscore, and dash", name)
	}
	return nil
}

func DefaultRoot() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "wafers"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "wafers"), nil
}

func OpenDefault() (*Store, error) {
	root, err := DefaultRoot()
	if err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func (s *Store) Lock() (func(), error) {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.Root, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func (s *Store) WaferDir(name string) string {
	return filepath.Join(s.Root, "wafers", name)
}

func (s *Store) InitWaferDirs(meta *Meta) error {
	for _, dir := range []string{meta.Upperdir, meta.Workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Save(meta *Meta) error {
	if err := os.MkdirAll(s.WaferDir(meta.Name), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(s.WaferDir(meta.Name), "meta.json"), data, 0o644)
}

func (s *Store) Load(name string) (*Meta, error) {
	data, err := os.ReadFile(filepath.Join(s.WaferDir(name), "meta.json"))
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) List() ([]Meta, []string, error) {
	root := filepath.Join(s.Root, "wafers")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var metas []Meta
	var bad []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := s.Load(entry.Name())
		if err != nil {
			bad = append(bad, entry.Name())
			continue
		}
		metas = append(metas, *meta)
	}
	return metas, bad, nil
}

func (s *Store) Remove(name string) error {
	return os.RemoveAll(s.WaferDir(name))
}

func UpperHasUserChanges(upperdir string) (bool, error) {
	empty := true
	err := filepath.WalkDir(upperdir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == upperdir {
			return nil
		}
		rel, err := filepath.Rel(upperdir, path)
		if err != nil {
			return err
		}
		if rel == ".git" || hasPathPrefix(rel, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if isInternalWhiteout(name) {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		empty = false
		return filepath.SkipAll
	})
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return !empty, err
}

func isInternalWhiteout(name string) bool {
	return name == ".wh..git" || name == ".wh..wh..opq"
}

func hasPathPrefix(path, prefix string) bool {
	return len(path) > len(prefix) && path[:len(prefix)] == prefix && os.IsPathSeparator(path[len(prefix)])
}
