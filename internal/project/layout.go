// Package project discovers Go modules within an isolated repository snapshot.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

var (
	// ErrInvalidModule indicates missing or malformed module/workspace configuration.
	ErrInvalidModule = errors.New("invalid Go module configuration")
	// ErrUnsupportedLayout indicates configuration that cannot be isolated within a snapshot.
	ErrUnsupportedLayout = errors.New("unsupported repository layout")
)

// Module identifies a repository-local Go module.
type Module struct {
	Directory string
	Path      string
}

// Workspace describes the root go.work and its explicit member directories.
type Workspace struct {
	File    string
	Members map[string]bool
}

// Layout contains every discovered module and the optional root workspace.
type Layout struct {
	Root      string
	Modules   []Module
	Workspace *Workspace
}

// Discover reads configuration without modifying it or invoking Go tooling.
// Fixture, vendor, hidden and underscore directories are excluded from discovery.
func Discover(dir string) (_ Layout, err error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve project root: %w", err)
	}

	directory, err := os.OpenRoot(root)
	if err != nil {
		return Layout{}, fmt.Errorf("%w: open project: %w", ErrInvalidModule, err)
	}
	defer func() { err = errors.Join(err, directory.Close()) }()

	layout := Layout{Root: root}
	modules, err := layout.discoverModules(directory)
	if err != nil {
		return Layout{}, err
	}

	if len(layout.Modules) == 0 {
		_, err = directory.ReadFile("go.mod")
		return Layout{}, fmt.Errorf("%w: repository contains no Go modules: %w", ErrInvalidModule, err)
	}

	for _, module := range layout.Modules {
		if err = validateReplacements(root, module.Directory, modules[module.Directory].Replace, modules); err != nil {
			return Layout{}, err
		}
	}

	if err = layout.readWorkspace(directory, modules); err != nil {
		return Layout{}, err
	}

	return layout, nil
}

func (layout *Layout) discoverModules(directory *os.Root) (map[string]*modfile.File, error) {
	modules := make(map[string]*modfile.File)
	err := filepath.WalkDir(layout.Root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if path == layout.Root {
			return nil
		}

		if entry.IsDir() {
			if ignoredDirectory(entry.Name()) {
				return fs.SkipDir
			}

			return nil
		}

		if entry.Name() == "go.work" && filepath.Dir(path) != layout.Root {
			return fmt.Errorf("%w: nested workspace %s; use a root go.work", ErrUnsupportedLayout, path)
		}

		if entry.Name() != "go.mod" {
			return nil
		}

		relative, err := filepath.Rel(layout.Root, path)
		if err != nil {
			return err
		}

		module, err := readModule(directory, relative)
		if err != nil {
			return err
		}

		moduleDir := filepath.Dir(path)
		modules[moduleDir] = module
		layout.Modules = append(layout.Modules, Module{Directory: moduleDir, Path: module.Module.Mod.Path})
		return nil
	})
	return modules, err
}

func ignoredDirectory(name string) bool {
	return name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func readModule(directory *os.Root, path string) (*modfile.File, error) {
	source, err := directory.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrInvalidModule, path, err)
	}

	module, err := modfile.Parse(path, source, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: parse %s: %w", ErrInvalidModule, path, err)
	}

	if module.Module == nil {
		return nil, fmt.Errorf("%w: %s must declare a module", ErrInvalidModule, path)
	}

	return module, nil
}

func (layout *Layout) readWorkspace(directory *os.Root, modules map[string]*modfile.File) error {
	filename := filepath.Join(layout.Root, "go.work")
	source, err := directory.ReadFile("go.work")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("%w: read workspace: %w", ErrInvalidModule, err)
	}

	work, err := modfile.ParseWork(filename, source, nil)
	if err != nil {
		return fmt.Errorf("%w: parse workspace: %w", ErrInvalidModule, err)
	}

	workspace := &Workspace{File: filename, Members: make(map[string]bool)}
	for _, use := range work.Use {
		target, err := localModule(layout.Root, layout.Root, use.Path, modules)
		if err != nil {
			return err
		}

		workspace.Members[target] = true
	}

	if len(workspace.Members) == 0 {
		return fmt.Errorf("%w: root workspace has no modules", ErrInvalidModule)
	}

	if err = validateReplacements(layout.Root, layout.Root, work.Replace, modules); err != nil {
		return err
	}

	layout.Workspace = workspace
	return nil
}

func validateReplacements(root, base string, replacements []*modfile.Replace, modules map[string]*modfile.File) error {
	for _, replacement := range replacements {
		if replacement.New.Version != "" {
			continue
		}

		if _, err := localModule(root, base, replacement.New.Path, modules); err != nil {
			return err
		}
	}

	return nil
}

func localModule(root, base, path string, modules map[string]*modfile.File) (string, error) {
	target := filepath.Join(base, path)
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(path) || !filepath.IsLocal(relative) {
		return "", fmt.Errorf("%w: module path %q must be relative and stay inside the repository", ErrUnsupportedLayout, path)
	}

	if modules[target] == nil {
		return "", fmt.Errorf("%w: module path %q does not name a discovered module", ErrInvalidModule, path)
	}

	return target, nil
}
