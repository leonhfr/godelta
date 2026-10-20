package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Remote identifies the immutable revisions and advertised PR head to fetch.
type Remote struct {
	URL        string
	BaseCommit string
	HeadCommit string
	HeadRef    string
}

var (
	// ErrFetch indicates that remote comparison objects could not be fetched.
	ErrFetch = errors.New("fetch comparison revisions")
	// ErrRevisionChanged indicates that the PR moved after its metadata was read.
	ErrRevisionChanged = errors.New("pull request head changed; retry the comparison")
	commitPattern      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	pullRefPattern     = regexp.MustCompile(`^refs/pull/[1-9][0-9]*/head$`)
)

// PrepareRemote fetches into a temporary bare repository and owns its snapshots.
func PrepareRemote(ctx context.Context, remote Remote) (*Comparison, error) {
	if !commitPattern.MatchString(remote.BaseCommit) || !commitPattern.MatchString(remote.HeadCommit) ||
		!pullRefPattern.MatchString(remote.HeadRef) || remote.URL == "" || strings.HasPrefix(remote.URL, "-") {
		return nil, fmt.Errorf("%w: invalid remote revisions", ErrInvalidOptions)
	}

	temporary, err := os.MkdirTemp("", "godelta-")
	if err != nil {
		return nil, fmt.Errorf("create remote snapshots: %w", err)
	}

	comparison := &Comparison{
		temporary:  temporary,
		Before:     filepath.Join(temporary, "before"),
		After:      filepath.Join(temporary, "after"),
		BaseCommit: remote.BaseCommit,
		HeadCommit: remote.HeadCommit,
		Mode:       Committed,
	}
	repo := repository{root: temporary}
	bare := filepath.Join(temporary, "repository.git")
	if _, err = repo.run(ctx, "init", "--bare", "--template=", "--object-format=sha1", bare); err == nil {
		repo.root = bare
		_, err = repo.run(
			ctx,
			"-c",
			"credential.helper=",
			"-c",
			"credential.helper=!gh auth git-credential",
			"fetch",
			"--depth=1",
			"--no-tags",
			"--no-write-fetch-head",
			"--no-recurse-submodules",
			"--no-auto-maintenance",
			"--force",
			"--",
			remote.URL,
			remote.BaseCommit+":refs/godelta/base",
			remote.HeadRef+":refs/godelta/head",
		)
	}

	if err != nil {
		return nil, errors.Join(fmt.Errorf("%w: %w", ErrFetch, err), comparison.Close())
	}

	head, err := repo.commit(ctx, "refs/godelta/head")
	if err == nil && head != remote.HeadCommit {
		err = ErrRevisionChanged
	}

	if err == nil {
		err = repo.materialize(ctx, comparison)
	}

	if err != nil {
		return nil, errors.Join(err, comparison.Close())
	}

	return comparison, nil
}
