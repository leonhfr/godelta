package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	urfave "github.com/urfave/cli/v3"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/cli"
	"github.com/leonhfr/godelta/internal/snapshot"
)

func TestCommandLocalModes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		args    []string
		mode    string
		working bool
		empty   bool
	}{
		{name: "working tree", mode: "working-tree", working: true},
		{name: "staged", args: []string{"--staged"}, mode: "staged"},
		{name: "branch", args: []string{"--base", "HEAD"}, mode: "working-tree", working: true},
		{name: "committed", args: []string{"--base", "HEAD", "--head", "HEAD"}, mode: "committed", empty: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repo := repository(t)
			write(
				t,
				repo,
				"source.go",
				"package fixture\nfunc Caller(){ Changed() }\nfunc Changed(){ Added() }\nfunc Added(){}\nfunc Unrelated(){}\n",
			)
			git(t, repo, "add", "source.go")
			content, err := os.ReadFile(filepath.Join(repo, "source.go"))
			require.NoError(t, err)

			write(t, repo, "source.go", string(content)+"func Working(){}\n")
			write(t, repo, "untracked.go", "package fixture\nfunc Untracked(){}\n")
			index, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			require.NoError(t, err)

			var stdout, stderr bytes.Buffer
			require.NoError(t, testCommand(repo, &stdout, &stderr).Run(t.Context(), append([]string{"godelta"}, test.args...)))
			assert.Contains(t, stderr.String(), "comparison: "+test.mode)
			assert.Contains(t, stderr.String(), "build: ")
			assert.True(t, strings.HasPrefix(stdout.String(), "flowchart LR\n"))
			assert.NotContains(t, stdout.String(), "comparison:")

			if test.empty {
				assert.Contains(t, stdout.String(), "No function changes")
				assert.Contains(t, stderr.String(), "No function changes.")
			} else {
				for _, label := range []string{"Caller #183; unchanged", "Changed #183; modified", "Added #183; added", "Removed #183; removed"} {
					assert.Contains(t, stdout.String(), label)
				}

				assert.NotContains(t, stdout.String(), "Unrelated")

				if test.working {
					assert.Contains(t, stdout.String(), "Working #183; added")
					assert.Contains(t, stdout.String(), "Untracked #183; added")
				} else {
					assert.NotContains(t, stdout.String(), "Working")
					assert.NotContains(t, stdout.String(), "Untracked")
				}
			}

			var repeated, diagnostics bytes.Buffer
			require.NoError(t, testCommand(repo, &repeated, &diagnostics).Run(t.Context(), append([]string{"godelta"}, test.args...)))
			assert.Equal(t, stdout.String(), repeated.String())

			afterIndex, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			require.NoError(t, err)
			assert.Equal(t, index, afterIndex)
		})
	}
}

func TestCommandArgumentErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want error
	}{
		{args: []string{"--format", "json"}, want: cli.ErrUsage},
		{args: []string{"pr", "123", "--format", "json"}, want: cli.ErrUsage},
		{args: []string{"123"}, want: cli.ErrUsage},
		{args: []string{"pr"}, want: cli.ErrUsage},
		{args: []string{"pr", "0"}, want: cli.ErrUsage},
		{args: []string{"pr", "+1"}, want: cli.ErrUsage},
		{args: []string{"pr", "01"}, want: cli.ErrUsage},
		{args: []string{"pr", "invalid"}, want: cli.ErrUsage},
		{args: []string{"pr", "999999999999999999999999999"}, want: cli.ErrUsage},
		{args: []string{"pr", "123", "456"}, want: cli.ErrUsage},
		{args: []string{"--staged", "pr", "123"}, want: cli.ErrUsage},
		{args: []string{"--base", "main", "pr", "123"}, want: cli.ErrUsage},
		{args: []string{"--head", "HEAD", "pr", "123"}, want: cli.ErrUsage},
		{args: []string{"--staged", "--base", "HEAD"}, want: snapshot.ErrInvalidOptions},
		{args: []string{"--head", "HEAD"}, want: snapshot.ErrInvalidOptions},
	}
	for _, test := range cases {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			require.ErrorIs(t, testCommand(t.TempDir(), &stdout, &stderr).Run(t.Context(), append([]string{"godelta"}, test.args...)), test.want)
			assert.Empty(t, stdout.String())
		})
	}
}

func TestCommandContextAndDynamicDiagnostics(t *testing.T) {
	t.Parallel()

	repo := repository(t)
	var source strings.Builder
	source.WriteString("package fixture\nfunc Changed(callback func()) { callback(); ")
	for index := range 55 {
		fmt.Fprintf(&source, "Context%02d(); ", index)
	}

	source.WriteString("}\n")
	for index := range 56 {
		fmt.Fprintf(&source, "func Context%02d(){}\n", index)
	}

	write(t, repo, "source.go", source.String())
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--no-gpg-sign", "-m", "context fixture")
	write(t, repo, "source.go", strings.Replace(source.String(), "callback();", "callback(); Context55();", 1))
	var stdout, stderr bytes.Buffer
	require.NoError(t, testCommand(repo, &stdout, &stderr).Run(t.Context(), []string{"godelta"}))
	assert.Contains(t, stderr.String(), "omitted 6 unchanged context functions (limit 50)")
	assert.Contains(t, stderr.String(), "unresolved calls: .:Changed (before 1; after 1)")
	assert.NotContains(t, stdout.String(), "Context50")
	assert.NotContains(t, stdout.String(), "omitted")
	assert.Equal(t, 51, strings.Count(stdout.String(), ":::unchanged"))
}

func TestCommandStreamFailures(t *testing.T) {
	t.Parallel()

	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			var stdout io.Writer = &output
			stderr := io.Discard
			if stream == "stdout" {
				stdout = failingWriter{}
			} else {
				stderr = failingWriter{}
			}

			require.ErrorIs(t, testCommand(repository(t), stdout, stderr).Run(t.Context(), []string{"godelta"}), io.ErrClosedPipe)
			assert.Empty(t, output.String())
		})
	}
}

//nolint:paralleltest // Changes process-wide flags and temporary directory to verify cleanup and build tags.
func TestCommandBuildTagsAndFailureCleanup(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	t.Setenv("GOFLAGS", "-tags=godelta_fixture")
	repo := repository(t)
	write(t, repo, "tagged.go", "//go:build godelta_fixture\n\npackage fixture\nfunc Tagged(){}\n")
	git(t, repo, "add", "--all")
	git(t, repo, "commit", "--no-gpg-sign", "-m", "tagged fixture")
	write(t, repo, "tagged.go", "//go:build godelta_fixture\n\npackage fixture\nfunc Tagged(){Changed()}\n")
	var stdout, stderr bytes.Buffer
	require.NoError(t, testCommand(repo, &stdout, &stderr).Run(t.Context(), []string{"godelta"}))
	assert.Contains(t, stdout.String(), "Tagged #183; modified")
	assert.Contains(t, stderr.String(), "GOFLAGS=\"-tags=godelta_fixture\"")

	write(t, repo, "source.go", "package fixture\nfunc Broken(){ missing() }\n")
	stdout.Reset()
	stderr.Reset()
	require.ErrorIs(t, testCommand(repo, &stdout, &stderr).Run(t.Context(), []string{"godelta"}), analysis.ErrLoadPackages)
	assert.Empty(t, stdout.String())

	matches, err := filepath.Glob(filepath.Join(temporary, "godelta-*"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestCommandTerminalFormat(t *testing.T) {
	t.Parallel()

	repo := repository(t)
	write(t, repo, "source.go", "package fixture\nfunc Caller(){Changed()}\nfunc Changed(){Added()}\nfunc Added(){}\nfunc Unrelated(){}\n")
	var stdout, stderr bytes.Buffer
	require.NoError(t, testCommand(repo, &stdout, &stderr).Run(t.Context(), []string{"godelta", "--format", "terminal"}))
	assert.Contains(t, stdout.String(), "Change group 1")
	assert.Contains(t, stdout.String(), "Caller · unchanged")
	assert.Contains(t, stdout.String(), "retained")
	assert.Contains(t, stdout.String(), "Changed · modified")
	assert.Contains(t, stdout.String(), "Added · added")
	assert.Contains(t, stdout.String(), "Removed · removed")
	assert.NotContains(t, stdout.String(), "Unrelated")
	assert.NotContains(t, stdout.String(), "flowchart")
	assert.NotContains(t, stdout.String(), "\x1b")
	assert.Contains(t, stderr.String(), "comparison: working-tree")
	require.ErrorIs(t, testCommand(repo, failingWriter{}, io.Discard).Run(t.Context(), []string{"godelta", "--format", "terminal"}), io.ErrClosedPipe)

	stdout.Reset()
	require.NoError(t, testCommand(repo, &stdout, io.Discard).Run(t.Context(), []string{"godelta", "--format", "mermaid"}))
	assert.True(t, strings.HasPrefix(stdout.String(), "flowchart LR\n"))
}

func repository(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, "init", "--initial-branch=main")
	git(t, dir, "config", "user.name", "Godelta Tests")
	git(t, dir, "config", "user.email", "godelta@example.invalid")
	git(t, dir, "config", "core.hooksPath", os.DevNull)
	git(t, dir, "config", "core.excludesFile", os.DevNull)
	write(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.27.1\n")
	write(t, dir, "source.go", "package fixture\nfunc Caller(){Changed()}\nfunc Changed(){}\nfunc Unrelated(){}\nfunc Removed(){}\n")
	git(t, dir, "add", "--all")
	git(t, dir, "commit", "--no-gpg-sign", "-m", "fixture")
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.CommandContext(t.Context(), "git", args...)
	command.Dir = dir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}

	command.Env = append(command.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, output)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

func testCommand(directory string, stdout, stderr io.Writer) *urfave.Command {
	command := cli.New(cli.WithDirectory(directory))
	command.Writer = stdout
	command.ErrWriter = stderr
	return command
}
