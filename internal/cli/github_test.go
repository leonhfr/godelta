package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/cli"
	"github.com/leonhfr/godelta/internal/github"
)

//nolint:paralleltest // Installs isolated gh and Git configuration through process-wide environment variables.
func TestRunPullRequest(t *testing.T) {
	repo := repository(t)
	ancestor := commandOutput(t, repo, "git", "rev-parse", "HEAD")
	git(t, repo, "checkout", "-b", "feature")
	write(t, repo, "source.go", "package fixture\nfunc Caller(){Changed()}\nfunc Changed(){Added()}\nfunc Added(){}\nfunc Unrelated(){}\n")
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--no-gpg-sign", "-m", "feature")
	head := commandOutput(t, repo, "git", "rev-parse", "HEAD")
	git(t, repo, "update-ref", "refs/pull/123/head", head)
	git(t, repo, "checkout", "main")
	write(t, repo, "base.go", "package fixture\nfunc BaseOnly(){}\n")
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--no-gpg-sign", "-m", "base")
	base := commandOutput(t, repo, "git", "rev-parse", "HEAD")
	write(t, repo, "dirty.go", "package fixture\nfunc Dirty(){}\n")
	originalIndex, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	require.NoError(t, err)

	originalRefs := commandOutput(t, repo, "git", "show-ref")
	var local, localDiagnostics bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), repo, []string{"--base", "main", "--head", "feature"}, &local, &localDiagnostics))

	// Preserve Go caches while isolating global Git configuration under a temporary HOME.
	var caches map[string]string
	require.NoError(t, json.Unmarshal([]byte(commandOutput(t, repo, "go", "env", "-json", "GOCACHE", "GOMODCACHE")), &caches))

	for name, value := range caches {
		t.Setenv(name, value)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	config := exec.CommandContext(
		t.Context(),
		"git",
		"config",
		"--file",
		filepath.Join(home, ".gitconfig"),
		"url."+repo+".insteadOf",
		"https://github.com/owner/project.git",
	)
	output, err := config.CombinedOutput()
	require.NoError(t, err, "%s", output)

	fixture := t.TempDir()
	t.Setenv("GODELTA_FIXTURE", fixture)
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
case "$1" in
repo) cat "$GODELTA_FIXTURE/repo.json" ;;
pr) cat "$GODELTA_FIXTURE/pr.json" ;;
api) cat "$GODELTA_FIXTURE/compare.json" ;;
*) exit 99 ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(fixture, "gh"), []byte(script), 0o700))

	writeJSON(t, fixture, "repo.json", map[string]any{"nameWithOwner": "owner/project", "url": "https://github.com/owner/project"})
	writeJSON(
		t,
		fixture,
		"pr.json",
		map[string]any{"number": 123, "url": "https://github.com/owner/project/pull/123", "baseRefOid": base, "headRefOid": head},
	)
	writeJSON(t, fixture, "compare.json", map[string]any{"merge_base_commit": map[string]string{"sha": ancestor}})
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	var stdout, stderr bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), t.TempDir(), []string{"pr", "123", "--repo", "owner/project"}, &stdout, &stderr))
	assert.Equal(t, local.String(), stdout.String())
	assert.Contains(t, stderr.String(), "pull request: https://github.com/owner/project/pull/123")
	assert.Contains(t, stderr.String(), ancestor)
	assert.Contains(t, stderr.String(), head)
	assert.NotContains(t, stdout.String(), "Dirty")
	assert.NotContains(t, stdout.String(), "BaseOnly")

	afterIndex, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
	require.NoError(t, err)
	assert.Equal(t, originalIndex, afterIndex)
	assert.Equal(t, originalRefs, commandOutput(t, repo, "git", "show-ref"))

	var localTree, prTree bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), repo, []string{"--base", "main", "--head", "feature", "--format", "terminal"}, &localTree, &stderr))
	require.NoError(t, cli.Run(t.Context(), t.TempDir(), []string{"pr", "123", "--repo", "owner/project", "--format", "terminal"}, &prTree, &stderr))
	assert.Equal(t, localTree.String(), prTree.String())
	assert.Contains(t, prTree.String(), "Changed · modified")
	assert.NotContains(t, prTree.String(), "flowchart LR")

	write(t, fixture, "pr.json", "invalid JSON")
	stdout.Reset()
	require.ErrorIs(t, cli.Run(t.Context(), t.TempDir(), []string{"pr", "123"}, &stdout, &stderr), github.ErrInvalidResponse)
	assert.Empty(t, stdout.String())

	matches, err := filepath.Glob(filepath.Join(temporary, "godelta-*"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestRunPRHelpOutsideRepository(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.NoError(t, cli.Run(t.Context(), t.TempDir(), []string{"pr", "--help"}, &stdout, &stderr))
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "repo")
}

func commandOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), name, args...)
	command.Dir = dir
	output, err := command.Output()
	require.NoError(t, err)

	return string(bytes.TrimSpace(output))
}

func writeJSON(t *testing.T, dir, name string, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)

	write(t, dir, name, string(data))
}
