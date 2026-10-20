// Package snapshot prepares isolated Git revisions for Go analysis.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Mode identifies the source of the new side of a comparison.
type Mode string

const (
	// WorkingTree includes staged, unstaged, and nonignored untracked files.
	WorkingTree Mode = "working-tree"
	// Staged includes only content recorded in the index.
	Staged Mode = "staged"
	// Committed compares two committed revisions.
	Committed Mode = "committed"
)

// Options selects a local Git comparison.
// Base uses merge-base semantics; Head requires Base and selects committed mode.
// Staged cannot be combined with Base or Head.
type Options struct {
	Staged bool
	Base   string
	Head   string
}

// Comparison owns temporary module directories until Close is called.
// HeadCommit is the resolved head underlying the selected comparison mode.
type Comparison struct {
	Before     string
	After      string
	BaseCommit string
	HeadCommit string
	Mode       Mode
	temporary  string
}

// Prepare copies both sides without modifying the repository, index, or refs.
// dir may be anywhere inside a checkout whose root contains a single Go module.
func Prepare(ctx context.Context, dir string, options Options) (*Comparison, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}

	repo, err := openRepository(ctx, dir)
	if err != nil {
		return nil, err
	}

	comparison, err := repo.resolve(ctx, options)
	if err != nil {
		return nil, err
	}

	comparison.temporary, err = os.MkdirTemp("", "godelta-")
	if err != nil {
		return nil, fmt.Errorf("create snapshot directory: %w", err)
	}

	comparison.Before = filepath.Join(comparison.temporary, "before")
	comparison.After = filepath.Join(comparison.temporary, "after")
	if err = repo.materialize(ctx, comparison); err != nil {
		return nil, errors.Join(err, comparison.Close())
	}

	return comparison, nil
}

// Close removes both snapshots and can be called more than once.
func (comparison *Comparison) Close() error {
	if err := os.RemoveAll(comparison.temporary); err != nil {
		return fmt.Errorf("remove snapshots: %w", err)
	}

	return nil
}

func (options Options) validate() error {
	if options.Staged && (options.Base != "" || options.Head != "") {
		return fmt.Errorf("%w: staged mode cannot be combined with base or head", ErrInvalidOptions)
	}

	if options.Head != "" && options.Base == "" {
		return fmt.Errorf("%w: head requires a base revision", ErrInvalidOptions)
	}

	return nil
}
