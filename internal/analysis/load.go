package analysis

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
		return Snapshot{}, fmt.Errorf("expected a module root with go.mod: %w", err)
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
		return Snapshot{}, fmt.Errorf("load Go packages: %w", err)
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
	var messages []string
	packages.Visit(
		pkgs,
		func(pkg *packages.Package) bool {
			for _, err := range pkg.Errors {
				messages = append(messages, err.Error())
			}

			return true
		},
		nil,
	)

	if len(messages) == 0 {
		return nil
	}

	slices.Sort(messages)

	return errors.New("load Go packages:\n" + strings.Join(slices.Compact(messages), "\n"))
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
