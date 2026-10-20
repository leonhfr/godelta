package snapshot_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/snapshot"
)

func TestPrepareRemoteEquivalent(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	ancestor := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-b", "feature")
	write(t, repo, "feature.go", "package fixture\nfunc Feature() { Original() }\n")
	commit(t, repo)
	head := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "update-ref", "refs/pull/123/head", head)
	git(t, repo, "checkout", "main")
	write(t, repo, "main-only.go", "package fixture\nfunc MainOnly() {}\n")
	commit(t, repo)
	write(t, repo, "dirty.go", "package fixture\nfunc Dirty() {}\n")
	original := state(t, repo)
	refs := git(t, repo, "show-ref")
	remote, err := snapshot.PrepareRemote(
		t.Context(),
		snapshot.Remote{URL: repo, BaseCommit: ancestor, HeadCommit: head, HeadRef: "refs/pull/123/head"},
	)
	require.NoError(t, err)

	t.Cleanup(func() { closeComparison(t, remote) })
	local, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{Base: "main", Head: "feature"})
	require.NoError(t, err)

	t.Cleanup(func() { closeComparison(t, local) })
	var changes []delta.Delta
	for _, comparison := range []*snapshot.Comparison{remote, local} {
		before, loadErr := analysis.Load(t.Context(), comparison.Before)
		require.NoError(t, loadErr)

		after, loadErr := analysis.Load(t.Context(), comparison.After)
		require.NoError(t, loadErr)

		changes = append(changes, delta.Compare(before, after))
		assert.Equal(t, ancestor, comparison.BaseCommit)
		assert.Equal(t, head, comparison.HeadCommit)

		assertAbsent(t, comparison.After, "dirty.go", "main-only.go")
	}

	assert.Equal(t, changes[1], changes[0])
	assert.Equal(t, original, state(t, repo))
	assert.Equal(t, refs, git(t, repo, "show-ref"))

	root := filepath.Dir(remote.Before)
	closeComparison(t, remote)
	assertAbsent(t, root, "repository.git", "before", "after")
}

//nolint:paralleltest // Observes cleanup through the process-wide temporary directory.
func TestPrepareRemoteFailures(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	repo := fixtureRepository(t)
	head := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "update-ref", "refs/pull/1/head", head)
	for _, test := range []struct {
		name   string
		remote snapshot.Remote
		want   error
	}{
		{"invalid", snapshot.Remote{URL: repo, BaseCommit: "--help", HeadCommit: head, HeadRef: "refs/pull/1/head"}, snapshot.ErrInvalidOptions},
		{"missing ref", snapshot.Remote{URL: repo, BaseCommit: head, HeadCommit: head, HeadRef: "refs/pull/2/head"}, snapshot.ErrFetch},
		{"moved head", snapshot.Remote{URL: repo, BaseCommit: head, HeadCommit: strings.Repeat("a", 40), HeadRef: "refs/pull/1/head"}, snapshot.ErrRevisionChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			comparison, err := snapshot.PrepareRemote(t.Context(), test.remote)
			require.ErrorIs(t, err, test.want)
			assert.Nil(t, comparison)

			if errors.Is(test.want, snapshot.ErrFetch) {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
			}

			matches, globErr := filepath.Glob(filepath.Join(temporary, "godelta-*"))
			require.NoError(t, globErr)
			assert.Empty(t, matches)
		})
	}
}
