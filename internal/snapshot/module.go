package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

func validateModule(root string) error {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open snapshot module: %w", err)
	}

	source, readErr := directory.ReadFile("go.mod")
	err = errors.Join(readErr, directory.Close())
	if err != nil {
		return fmt.Errorf("%w: snapshot requires a go.mod at the repository root: %w", ErrInvalidModule, err)
	}

	module, err := modfile.Parse("go.mod", source, nil)
	if err != nil {
		return fmt.Errorf("%w: parse snapshot go.mod: %w", ErrInvalidModule, err)
	}

	if module.Module == nil {
		return fmt.Errorf("%w: snapshot go.mod must declare a module", ErrInvalidModule)
	}

	for _, replacement := range module.Replace {
		if replacement.New.Version == "" {
			return fmt.Errorf("%w: local module replacement %q", ErrUnsupportedLayout, replacement.New.Path)
		}
	}

	return validateLayout(root)
}

func validateLayout(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == root {
			return nil
		}

		if entry.IsDir() && (entry.Name() == "testdata" || entry.Name() == "vendor" ||
			strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_")) {
			return fs.SkipDir
		}

		if entry.Name() == "go.work" {
			return fmt.Errorf("%w: go workspaces are not supported; compare a single-module repository", ErrUnsupportedLayout)
		}

		if entry.Name() == "go.mod" && filepath.Dir(path) != root {
			return fmt.Errorf("%w: multiple Go modules: %s", ErrUnsupportedLayout, strings.TrimPrefix(path, root+string(filepath.Separator)))
		}

		return nil
	})
}
