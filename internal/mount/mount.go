package mount

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func LookPath(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func CheckFuseDevice() error {
	info, err := os.Stat("/dev/fuse")
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("/dev/fuse is a directory")
	}
	file, err := os.OpenFile("/dev/fuse", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return file.Close()
}

func EnsureEmptyMountpoint(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("mountpoint %s is not empty", path)
	}
	return nil
}

func FuseOverlay(ctx context.Context, lowerdir, upperdir, workdir, mountpoint string) error {
	args := []string{
		"-o", "lowerdir=" + lowerdir,
		"-o", "upperdir=" + upperdir,
		"-o", "workdir=" + workdir,
		mountpoint,
	}
	cmd := exec.CommandContext(ctx, "fuse-overlayfs", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fuse-overlayfs: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Unmount(ctx context.Context, mountpoint string) error {
	cmd := exec.CommandContext(ctx, "fusermount3", "-u", mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fusermount3 -u %s: %w: %s", mountpoint, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func IsMounted(mountpoint string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	target, err := filepath.Abs(mountpoint)
	if err != nil {
		return false
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 5 && unescapeMountInfo(fields[4]) == target {
			return true
		}
	}
	return false
}

func TestMount(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("requires Linux, got %s", runtime.GOOS)
	}
	root, err := os.MkdirTemp("", "wafers-doctor-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	lower := filepath.Join(root, "lower")
	upper := filepath.Join(root, "upper")
	work := filepath.Join(root, "work")
	mnt := filepath.Join(root, "mnt")
	for _, dir := range []string{lower, upper, work, mnt} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(lower, "probe"), []byte("ok\n"), 0o644); err != nil {
		return err
	}
	if err := FuseOverlay(ctx, lower, upper, work, mnt); err != nil {
		return err
	}
	defer Unmount(ctx, mnt)
	if _, err := os.Stat(filepath.Join(mnt, "probe")); err != nil {
		return err
	}
	return nil
}

func unescapeMountInfo(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(s)
}
