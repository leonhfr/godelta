// Package app coordinates Go diff analysis for review.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/github"
	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/snapshot"
)

// Build records the effective platform and flags used by Go tooling.
type Build struct {
	OS         string `json:"GOOS"`
	Arch       string `json:"GOARCH"`
	Flags      string `json:"GOFLAGS"`
	CGOEnabled string `json:"CGO_ENABLED"`
}

// Result contains the selected graph and comparison metadata for presentation.
type Result struct {
	PullRequestURL string
	Graph          graph.Graph
	Mode           snapshot.Mode
	BaseCommit     string
	HeadCommit     string
	Build          Build
}

// Review prepares and analyzes both revisions, selects context, and removes snapshots.
func Review(ctx context.Context, dir string, options snapshot.Options) (_ Result, err error) {
	comparison, err := snapshot.Prepare(ctx, dir, options)
	if err != nil {
		return Result{}, err
	}

	defer func() { err = errors.Join(err, comparison.Close()) }()

	return analyze(ctx, comparison)
}

// ReviewPR resolves a GitHub PR and analyzes isolated committed snapshots.
func ReviewPR(ctx context.Context, dir, repository string, number int) (_ Result, err error) {
	pr, err := (github.Client{}).Resolve(ctx, dir, repository, number)
	if err != nil {
		return Result{}, err
	}

	comparison, err := snapshot.PrepareRemote(ctx, pr.Remote)
	if err != nil {
		return Result{}, err
	}
	defer func() { err = errors.Join(err, comparison.Close()) }()

	result, err := analyze(ctx, comparison)
	result.PullRequestURL = pr.URL
	return result, err
}

func analyze(ctx context.Context, comparison *snapshot.Comparison) (Result, error) {
	build, err := buildConfiguration(ctx, comparison.Before)
	if err != nil {
		return Result{}, err
	}

	before, err := analysis.Load(ctx, comparison.Before)
	if err != nil {
		return Result{}, fmt.Errorf("analyze old snapshot: %w", err)
	}

	after, err := analysis.Load(ctx, comparison.After)
	if err != nil {
		return Result{}, fmt.Errorf("analyze new snapshot: %w", err)
	}

	return Result{
		Graph:      graph.Select(delta.Compare(before, after)),
		Mode:       comparison.Mode,
		BaseCommit: comparison.BaseCommit,
		HeadCommit: comparison.HeadCommit,
		Build:      build,
	}, nil
}

func buildConfiguration(ctx context.Context, dir string) (Build, error) {
	command := exec.CommandContext(ctx, "go", "env", "-json", "GOOS", "GOARCH", "GOFLAGS", "CGO_ENABLED")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.Output()
	if err != nil {
		return Build{}, fmt.Errorf("read Go build configuration: %w", err)
	}

	var build Build
	if err = json.Unmarshal(output, &build); err != nil {
		return Build{}, fmt.Errorf("decode Go build configuration: %w", err)
	}

	return build, nil
}
