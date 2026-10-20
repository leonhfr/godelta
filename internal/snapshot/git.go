package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type repository struct {
	root string
}

func openRepository(ctx context.Context, dir string) (repository, error) {
	repo := repository{root: dir}
	root, err := repo.run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return repository{}, fmt.Errorf("%w: %w", ErrNotRepository, err)
	}

	repo.root = strings.TrimSuffix(string(root), "\n")

	return repo, nil
}

func (repo repository) resolve(ctx context.Context, options Options) (*Comparison, error) {
	head := options.Head
	if head == "" {
		head = "HEAD"
	}

	headCommit, err := repo.commit(ctx, head)
	if err != nil {
		return nil, err
	}

	comparison := &Comparison{BaseCommit: headCommit, HeadCommit: headCommit, Mode: WorkingTree}
	if options.Base != "" {
		base, resolveErr := repo.commit(ctx, options.Base)
		if resolveErr != nil {
			return nil, resolveErr
		}

		ancestor, mergeErr := repo.run(ctx, "merge-base", base, headCommit)
		if mergeErr != nil {
			return nil, fmt.Errorf("%w: find merge base: %w", ErrInvalidRevision, mergeErr)
		}

		comparison.BaseCommit = strings.TrimSpace(string(ancestor))
	}

	if options.Head != "" {
		comparison.Mode = Committed
	} else if options.Staged {
		comparison.Mode = Staged
	}

	if comparison.Mode != Committed {
		if err = repo.checkIndex(ctx); err != nil {
			return nil, err
		}
	}

	return comparison, nil
}

func (repo repository) commit(ctx context.Context, ref string) (string, error) {
	output, err := repo.run(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%w: resolve %q (a committed revision is required): %w", ErrInvalidRevision, ref, err)
	}

	return strings.TrimSpace(string(output)), nil
}

func (repo repository) checkIndex(ctx context.Context) error {
	unmerged, err := repo.run(ctx, "ls-files", "--unmerged", "-z")
	if err != nil {
		return err
	}

	if len(unmerged) != 0 {
		return fmt.Errorf("%w: resolve conflicts before comparing the index or working tree", ErrMergeConflict)
	}

	entries, err := repo.run(ctx, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}

	for entry := range strings.SplitSeq(string(entries), "\x00") {
		if strings.HasPrefix(entry, "160000 ") {
			return fmt.Errorf("%w: submodules are not supported", ErrUnsupportedLayout)
		}
	}

	// Sparse checkouts omit tracked inputs needed for a complete working snapshot.
	output, err := repo.run(ctx, "config", "--get", "--bool", "core.sparseCheckout")
	if err == nil && strings.TrimSpace(string(output)) == "true" {
		return fmt.Errorf("%w: sparse checkouts are not supported", ErrUnsupportedLayout)
	}

	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return err
		}
	}

	return nil
}

func (repo repository) run(ctx context.Context, args ...string) ([]byte, error) {
	command := repo.command(ctx, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}

	return output, nil
}

func (repo repository) command(ctx context.Context, args ...string) *exec.Cmd {
	//nolint:gosec // Uses a fixed executable without a shell; revision inputs follow --end-of-options.
	command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false"}, args...)...)
	command.Dir = repo.root
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}

	command.Env = append(command.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1")

	return command
}
