package snapshot_test

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/snapshot"
)

func TestPrepareWorkingTreeAndStaged(t *testing.T) {
	t.Parallel()

	for _, mode := range []snapshot.Mode{snapshot.WorkingTree, snapshot.Staged} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()

			repo := fixtureRepository(t)
			head := git(t, repo, "rev-parse", "HEAD")
			stagedMain := read(t, repo, "main.go") + "\nfunc Staged() {}\n"
			write(t, repo, "main.go", stagedMain)
			write(t, repo, "added.go", "package fixture\nfunc Added() {}\n")
			git(t, repo, "add", "main.go", "added.go")
			git(t, repo, "rm", "remove.go")
			git(t, repo, "mv", "rename old.go", "renamed.go")
			workingMain := stagedMain + "\nfunc Working() {}\n"
			write(t, repo, "main.go", workingMain)
			write(t, repo, "added.go", "package fixture\nfunc AddedWorking() {}\n")
			write(t, repo, "untracked.go", "package fixture\nfunc Untracked() {}\n")
			write(t, repo, "ignored/invalid.go", "not Go source")
			write(t, repo, "intent.go", "package fixture\nfunc Intent() {}\n")
			git(t, repo, "add", "-N", "intent.go")
			write(t, repo, "tab\tnewline\n.txt", "untracked unusual path")
			remove(t, repo, "gone.go")
			original := state(t, repo)

			comparison, err := snapshot.Prepare(t.Context(), filepath.Join(repo, "assets"), snapshot.Options{Staged: mode == snapshot.Staged})
			require.NoError(t, err)

			t.Cleanup(func() { closeComparison(t, comparison) })
			assert.Equal(t, mode, comparison.Mode)
			assert.Equal(t, head, comparison.BaseCommit)
			assert.Equal(t, head, comparison.HeadCommit)

			assert.NotEqual(t, stagedMain, read(t, comparison.Before, "main.go"), "old snapshot contains staged edits")

			assertAbsent(t, comparison.Before, "added.go", "untracked.go", ".git")
			assertAbsent(t, comparison.After, "remove.go", "rename old.go", "ignored/invalid.go", ".git")
			assert.Equal(t, read(t, comparison.Before, "rename old.go"), read(t, comparison.After, "renamed.go"))

			assert.Equal(t, "embedded content\n", read(t, comparison.After, "assets/data.txt"))

			assert.Equal(t, "tracked unusual path", read(t, comparison.Before, "tracked\tnewline\n.txt"))

			if mode == snapshot.Staged {
				assert.Equal(t, stagedMain, read(t, comparison.After, "main.go"))
				assert.Contains(t, read(t, comparison.After, "added.go"), "func Added()")

				assertAbsent(t, comparison.After, "untracked.go", "intent.go", "tab\tnewline\n.txt")
				_ = read(t, comparison.After, "gone.go")
			} else {
				assert.Equal(t, workingMain, read(t, comparison.After, "main.go"))
				assert.Contains(t, read(t, comparison.After, "added.go"), "AddedWorking")

				_ = read(t, comparison.After, "untracked.go")
				_ = read(t, comparison.After, "intent.go")
				_ = read(t, comparison.After, "tab\tnewline\n.txt")
				assertAbsent(t, comparison.After, "gone.go")
			}

			for _, dir := range []string{comparison.Before, comparison.After} {
				_, err = analysis.Load(t.Context(), dir)
				require.NoError(t, err, "analyze snapshot")

				info, statErr := os.Stat(filepath.Join(dir, "tool.sh"))
				require.NoError(t, statErr)
				assert.NotZero(t, info.Mode().Perm()&0o111, "executable mode was not preserved")
			}

			assert.Equal(t, original, state(t, repo), "preparing/analyzing snapshots modified the checkout or index")

			closeComparison(t, comparison)
			assertAbsent(t, comparison.Before, "main.go")
			assertAbsent(t, comparison.After, "main.go")
		})
	}
}

func TestPrepareMergeBase(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	ancestor := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "-b", "feature")
	write(t, repo, "feature.go", "package fixture\nfunc Feature() {}\n")
	commit(t, repo)
	head := git(t, repo, "rev-parse", "HEAD")
	git(t, repo, "checkout", "main")
	write(t, repo, "main-only.go", "package fixture\nfunc MainOnly() {}\n")
	commit(t, repo)
	git(t, repo, "checkout", "feature")
	write(t, repo, "dirty.go", "package fixture\nfunc Dirty() {}\n")
	for _, committed := range []bool{false, true} {
		options := snapshot.Options{Base: "main"}
		if committed {
			options.Head = "feature"
			git(t, repo, "checkout", "main")
		}

		original := state(t, repo)

		comparison, err := snapshot.Prepare(t.Context(), repo, options)
		require.NoError(t, err)

		t.Cleanup(func() { closeComparison(t, comparison) })
		assert.Equal(t, ancestor, comparison.BaseCommit)
		assert.Equal(t, head, comparison.HeadCommit)

		assertAbsent(t, comparison.Before, "feature.go", "main-only.go", "dirty.go")
		assertAbsent(t, comparison.After, "main-only.go")
		_ = read(t, comparison.After, "feature.go")
		if committed {
			assertAbsent(t, comparison.After, "dirty.go")
			assert.Equal(t, snapshot.Committed, comparison.Mode)
		} else {
			_ = read(t, comparison.After, "dirty.go")
		}

		closeComparison(t, comparison)
		assert.Equal(t, original, state(t, repo), "merge-base comparison modified repository state")
	}
}

func TestPrepareOptionsAndRevisions(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	cases := []struct {
		name    string
		options snapshot.Options
		want    error
	}{
		{name: "staged base", options: snapshot.Options{Staged: true, Base: "main"}, want: snapshot.ErrInvalidOptions},
		{name: "staged head", options: snapshot.Options{Staged: true, Head: "HEAD"}, want: snapshot.ErrInvalidOptions},
		{name: "head without base", options: snapshot.Options{Head: "HEAD"}, want: snapshot.ErrInvalidOptions},
		{name: "missing base", options: snapshot.Options{Base: "missing"}, want: snapshot.ErrInvalidRevision},
		{name: "missing head", options: snapshot.Options{Base: "main", Head: "missing"}, want: snapshot.ErrInvalidRevision},
		{name: "option-like base", options: snapshot.Options{Base: "--help"}, want: snapshot.ErrInvalidRevision},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			comparison, err := snapshot.Prepare(t.Context(), repo, test.options)
			require.ErrorIs(t, err, test.want)
			assert.Nil(t, comparison)
		})
	}
}

func TestPrepareUnsupportedLayouts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{name: "missing module", files: map[string]string{"go.mod": ""}, want: snapshot.ErrInvalidModule},
		{name: "workspace", files: map[string]string{"go.work": "go 1.27.1\nuse .\n"}, want: snapshot.ErrUnsupportedLayout},
		{name: "nested module", files: map[string]string{"nested/go.mod": "module example.com/nested\n"}, want: snapshot.ErrUnsupportedLayout},
		{
			name:  "local replacement",
			files: map[string]string{"go.mod": "module example.com/fixture\nreplace example.com/other => ../other\n"},
			want:  snapshot.ErrUnsupportedLayout,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repo := fixtureRepository(t)
			for name, content := range test.files {
				write(t, repo, name, content)
			}

			original := state(t, repo)
			comparison, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
			require.ErrorIs(t, err, test.want)
			assert.Nil(t, comparison)

			assert.Equal(t, original, state(t, repo), "failed comparison modified repository state")
		})
	}
}

func TestPrepareUnbornAndNonRepository(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := snapshot.Prepare(t.Context(), dir, snapshot.Options{})
	require.ErrorIs(t, err, snapshot.ErrNotRepository)

	var gitFailure *exec.ExitError
	require.ErrorAs(t, err, &gitFailure)

	git(t, dir, "init", "--initial-branch=main")
	_, err = snapshot.Prepare(t.Context(), dir, snapshot.Options{})
	require.ErrorIs(t, err, snapshot.ErrInvalidRevision)
	require.ErrorAs(t, err, &gitFailure)
}

func TestPrepareConflicts(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	git(t, repo, "checkout", "-b", "feature")
	write(t, repo, "main.go", "package fixture\nfunc Feature() {}\n")
	commit(t, repo)
	git(t, repo, "checkout", "main")
	write(t, repo, "main.go", "package fixture\nfunc Main() {}\n")
	commit(t, repo)
	command := gitCommand(t.Context(), repo, "merge", "feature")
	require.Error(t, command.Run(), "expected merge conflict")

	original := state(t, repo)
	for _, options := range []snapshot.Options{{}, {Staged: true}} {
		_, err := snapshot.Prepare(t.Context(), repo, options)
		require.ErrorIs(t, err, snapshot.ErrMergeConflict)
	}

	assert.Equal(t, original, state(t, repo), "conflict detection modified repository state")
}

func TestPrepareSymlinks(t *testing.T) {
	t.Parallel()

	for _, committed := range []bool{false, true} {
		repo := fixtureRepository(t)
		require.NoError(t, os.Symlink("assets", filepath.Join(repo, "link")))

		if committed {
			commit(t, repo)
		}

		_, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
		require.ErrorIs(t, err, snapshot.ErrUnsupportedLayout)
	}
}

//nolint:paralleltest // Sets the process-wide temporary directory to observe cleanup.
func TestPrepareFailureCleanup(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	repo := fixtureRepository(t)
	write(t, repo, "go.work", "go 1.27.1\nuse .\n")
	_, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
	require.ErrorIs(t, err, snapshot.ErrUnsupportedLayout)

	matches, err := filepath.Glob(filepath.Join(temporary, "godelta-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "snapshots leaked after failure")
}

func TestPrepareSubmoduleAndSparseCheckout(t *testing.T) {
	t.Parallel()

	for _, layout := range []string{"submodules", "sparse checkouts"} {
		t.Run(layout, func(t *testing.T) {
			t.Parallel()

			repo := fixtureRepository(t)
			if layout == "submodules" {
				head := git(t, repo, "rev-parse", "HEAD")
				git(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+head+",submodule")
			} else {
				git(t, repo, "config", "core.sparseCheckout", "true")
			}

			for _, options := range []snapshot.Options{{}, {Staged: true}} {
				_, err := snapshot.Prepare(t.Context(), repo, options)
				require.ErrorIs(t, err, snapshot.ErrUnsupportedLayout)
			}
		})
	}
}

func TestPrepareFileDirectoryReplacements(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	write(t, repo, "was-file", "old file")
	write(t, repo, "was-directory/child", "old child")
	commit(t, repo)
	remove(t, repo, "was-file")
	write(t, repo, "was-file/new-child", "new child")
	remove(t, repo, "was-directory/child")
	remove(t, repo, "was-directory")
	write(t, repo, "was-directory", "new file")
	comparison, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
	require.NoError(t, err)

	t.Cleanup(func() { closeComparison(t, comparison) })
	assert.Equal(t, "old file", read(t, comparison.Before, "was-file"))
	assert.Equal(t, "new child", read(t, comparison.After, "was-file/new-child"))
	assert.Equal(t, "old child", read(t, comparison.Before, "was-directory/child"))
	assert.Equal(t, "new file", read(t, comparison.After, "was-directory"))
}

func TestPrepareMissingModuleCause(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	remove(t, repo, "go.mod")
	comparison, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
	require.ErrorIs(t, err, snapshot.ErrInvalidModule)
	require.ErrorIs(t, err, os.ErrNotExist)

	var pathError *os.PathError
	require.ErrorAs(t, err, &pathError)
	assert.Equal(t, "go.mod", filepath.Base(pathError.Path))
	assert.Nil(t, comparison)
}

func TestPrepareInvalidModuleCause(t *testing.T) {
	t.Parallel()

	repo := fixtureRepository(t)
	write(t, repo, "go.mod", "not a module declaration\n")
	comparison, err := snapshot.Prepare(t.Context(), repo, snapshot.Options{})
	require.ErrorIs(t, err, snapshot.ErrInvalidModule)

	var parseErrors modfile.ErrorList
	require.ErrorAs(t, err, &parseErrors)
	assert.NotEmpty(t, parseErrors)
	assert.Nil(t, comparison)
}

func fixtureRepository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "--initial-branch=main")
	git(t, dir, "config", "user.name", "Godelta Tests")
	git(t, dir, "config", "user.email", "godelta@example.invalid")
	git(t, dir, "config", "core.excludesFile", os.DevNull)
	git(t, dir, "config", "core.hooksPath", os.DevNull)
	files := map[string]string{
		".gitattributes":         "assets/data.txt export-ignore\n",
		".gitignore":             "ignored/\n",
		"go.mod":                 "module example.com/fixture\n\ngo 1.27.1\n",
		"main.go":                "package fixture\nimport _ \"embed\"\n//go:embed assets/data.txt\nvar data string\nfunc Original() {}\n",
		"remove.go":              "package fixture\nfunc Removed() {}\n",
		"gone.go":                "package fixture\nfunc Gone() {}\n",
		"rename old.go":          "package fixture\nfunc Renamed() {}\n",
		"assets/data.txt":        "embedded content\n",
		"tracked\tnewline\n.txt": "tracked unusual path",
		"tool.sh":                "#!/bin/sh\nexit 0\n",
	}
	for name, content := range files {
		write(t, dir, name, content)
	}

	require.NoError(t, os.Chmod(filepath.Join(dir, "tool.sh"), 0o755))

	commit(t, dir)

	return dir
}

func commit(t *testing.T, dir string) {
	t.Helper()

	git(t, dir, "add", "--all")
	git(t, dir, "commit", "--no-gpg-sign", "-m", "fixture")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	output, err := gitCommand(t.Context(), dir, args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, output)

	return strings.TrimSuffix(string(output), "\n")
}

func gitCommand(ctx context.Context, dir string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}

	command.Env = append(
		command.Env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
	)

	return command
}

func read(t *testing.T, dir, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(content)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()

	filename := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o750))

	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
}

func remove(t *testing.T, dir, name string) {
	t.Helper()

	require.NoError(t, os.Remove(filepath.Join(dir, name)))
}

func assertAbsent(t *testing.T, dir string, names ...string) {
	t.Helper()

	for _, name := range names {
		_, err := os.Stat(filepath.Join(dir, name))
		require.ErrorIs(t, err, os.ErrNotExist, "expected %q to be absent", name)
	}
}

func closeComparison(t *testing.T, comparison *snapshot.Comparison) {
	t.Helper()

	assert.NoError(t, comparison.Close())
}

func state(t *testing.T, dir string) map[string]string {
	t.Helper()

	result := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}

			return nil
		}

		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		result[relative] = read(t, dir, relative)

		return nil
	})
	require.NoError(t, err)

	result[".git/index"] = read(t, dir, ".git/index")
	result[".git/HEAD"] = read(t, dir, ".git/HEAD")
	result["head-commit"] = git(t, dir, "rev-parse", "HEAD")
	result["status"] = git(t, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")

	return result
}
