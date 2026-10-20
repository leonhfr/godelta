package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

func (repo repository) writeWorkingTree(ctx context.Context, dir string) error {
	output, err := repo.run(ctx, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}

	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	paths := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	slices.Sort(paths)
	for _, name := range slices.Compact(paths) {
		if name == "" {
			continue
		}

		if err = ctx.Err(); err != nil {
			return err
		}

		if err = repo.copyWorkingFile(dir, name); err != nil {
			return err
		}
	}

	return nil
}

func (repo repository) copyWorkingFile(dir, name string) error {
	err := validatePath(name)
	if err != nil {
		return err
	}

	info, err := workingFile(repo.root, name)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil
	}

	if err != nil {
		return err
	}

	// A tracked file replaced by a directory is absent; its new children are listed separately.
	if info.IsDir() {
		return nil
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: working-tree file %q: %s", ErrUnsupportedLayout, name, info.Mode())
	}

	source, err := os.Open(filepath.Join(repo.root, filepath.FromSlash(name)))
	if err != nil {
		return fmt.Errorf("open working-tree file %q: %w", name, err)
	}

	mode := os.FileMode(0o644)
	if info.Mode().Perm()&0o111 != 0 {
		mode = 0o755
	}

	copyErr := copyFile(dir, name, mode, source)

	return errors.Join(copyErr, source.Close())
}

func workingFile(root, name string) (os.FileInfo, error) {
	current := root
	var info os.FileInfo
	for part := range strings.SplitSeq(name, "/") {
		current = filepath.Join(current, part)
		var err error
		info, err = os.Lstat(current)
		if err != nil {
			return nil, err
		}

		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: symlinks are not supported: %q", ErrUnsupportedLayout, name)
		}
	}

	return info, nil
}

func validatePath(name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("%w: %q", ErrInvalidPath, name)
	}

	for part := range strings.SplitSeq(name, "/") {
		if strings.EqualFold(part, ".git") {
			return fmt.Errorf("%w: git metadata cannot be copied: %q", ErrInvalidPath, name)
		}
	}

	return nil
}

func copyFile(dir, name string, mode os.FileMode, source io.Reader) (err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open snapshot root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()

	name = filepath.FromSlash(name)
	if err = root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return fmt.Errorf("create snapshot parent: %w", err)
	}

	file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create snapshot file: %w", err)
	}

	_, copyErr := io.Copy(file, source)
	if err = errors.Join(copyErr, file.Chmod(mode), file.Close()); err != nil {
		return fmt.Errorf("copy snapshot file: %w", err)
	}

	return nil
}
