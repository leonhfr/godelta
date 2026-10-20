package analysis

import (
	"cmp"
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"

	"github.com/leonhfr/godelta/internal/project"
)

// Load analyzes all discovered repository modules under the current build environment.
// Root workspace members load together; other modules load independently.
// Package loading is read-only and never discovers workspaces outside the repository.
func Load(ctx context.Context, dir string) (Snapshot, error) {
	layout, err := project.Discover(dir)
	if err != nil {
		return Snapshot{}, err
	}

	inventory := newInventory(layout.Root)
	for _, group := range loadGroups(layout) {
		cfg := &packages.Config{
			Context:    ctx,
			Dir:        group.directory,
			Mode:       packages.LoadSyntax | packages.NeedModule,
			Env:        append(os.Environ(), "GOWORK="+group.workspace),
			BuildFlags: []string{"-mod=readonly"},
		}
		pkgs, err := packages.Load(cfg, group.patterns...)
		if err != nil {
			return Snapshot{}, fmt.Errorf("%w: %w", ErrLoadPackages, err)
		}

		if err = packageErrors(pkgs); err != nil {
			return Snapshot{}, err
		}

		inventory.rememberPackages(pkgs, layout)
		for _, pkg := range pkgs {
			if pkg.Module == nil || !pkg.Module.Main {
				continue
			}

			if err = inventory.addPackage(pkg); err != nil {
				return Snapshot{}, err
			}
		}
	}

	inventory.findCalls()
	return inventory.snapshot, nil
}

type loadGroup struct {
	directory string
	workspace string
	patterns  []string
}

func loadGroups(layout project.Layout) []loadGroup {
	var groups []loadGroup
	workspace := loadGroup{directory: layout.Root}
	if layout.Workspace != nil {
		workspace.workspace = layout.Workspace.File
	}

	for _, module := range layout.Modules {
		if layout.Workspace != nil && layout.Workspace.Members[module.Directory] {
			relative, _ := filepath.Rel(layout.Root, module.Directory)
			pattern := "./..."
			if relative != "." {
				pattern = "./" + filepath.ToSlash(relative) + "/..."
			}

			workspace.patterns = append(workspace.patterns, pattern)
		} else {
			groups = append(groups, loadGroup{directory: module.Directory, workspace: "off", patterns: []string{"./..."}})
		}
	}

	if len(workspace.patterns) != 0 {
		groups = append(groups, workspace)
	}

	return groups
}

func packageErrors(pkgs []*packages.Package) error {
	var diagnostics []packages.Error
	packages.Visit(
		pkgs,
		func(pkg *packages.Package) bool {
			diagnostics = append(diagnostics, pkg.Errors...)

			return true
		},
		nil,
	)

	if len(diagnostics) == 0 {
		return nil
	}

	slices.SortFunc(diagnostics, func(left, right packages.Error) int {
		if message := cmp.Compare(left.Error(), right.Error()); message != 0 {
			return message
		}

		return cmp.Compare(left.Kind, right.Kind)
	})
	diagnostics = slices.Compact(diagnostics)
	return &PackageErrors{Diagnostics: diagnostics}
}

type inventory struct {
	root        string
	snapshot    Snapshot
	bodies      []functionBody
	objects     map[functionSymbol]ID
	directories map[*types.Package]string
	literals    map[*ast.FuncLit]ID
}

func newInventory(root string) *inventory {
	return &inventory{
		root:        root,
		snapshot:    Snapshot{Functions: make(map[ID]Function)},
		objects:     make(map[functionSymbol]ID),
		directories: make(map[*types.Package]string),
		literals:    make(map[*ast.FuncLit]ID),
	}
}

type functionBody struct {
	id   ID
	pkg  *packages.Package
	body *ast.BlockStmt
}
