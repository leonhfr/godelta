package analysis

import (
	"cmp"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/tools/go/packages"
)

// Load analyzes the module rooted at dir using the current Go build environment.
// It excludes test and generated functions and never runs project code.
// Package loading uses read-only module mode and disables workspace discovery.
func Load(ctx context.Context, dir string) (Snapshot, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve module directory: %w", err)
	}

	if _, err = os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return Snapshot{}, fmt.Errorf("%w: expected a directory with go.mod: %w", ErrInvalidModule, err)
	}

	cfg := &packages.Config{
		Context:    ctx,
		Dir:        root,
		Mode:       packages.LoadSyntax | packages.NeedModule,
		Env:        append(os.Environ(), "GOWORK=off"),
		BuildFlags: []string{"-mod=readonly"},
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %w", ErrLoadPackages, err)
	}

	if err = packageErrors(pkgs); err != nil {
		return Snapshot{}, err
	}

	inventory := newInventory(root)
	for _, pkg := range pkgs {
		if pkg.Module == nil || !pkg.Module.Main {
			continue
		}

		if err = inventory.addPackage(pkg); err != nil {
			return Snapshot{}, err
		}
	}

	inventory.findCalls()

	return inventory.snapshot, nil
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
	root     string
	snapshot Snapshot
	bodies   []functionBody
	objects  map[token.Pos]ID
	literals map[*ast.FuncLit]ID
}

func newInventory(root string) *inventory {
	return &inventory{
		root:     root,
		snapshot: Snapshot{Functions: make(map[ID]Function)},
		objects:  make(map[token.Pos]ID),
		literals: make(map[*ast.FuncLit]ID),
	}
}

type functionBody struct {
	id   ID
	pkg  *packages.Package
	body *ast.BlockStmt
}
