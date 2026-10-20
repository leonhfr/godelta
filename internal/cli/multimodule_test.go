package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandMultiModuleComparisons(t *testing.T) {
	t.Parallel()

	for _, workspace := range []bool{false, true} {
		name := "replacements"
		if workspace {
			name = "workspace"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := repository(t)
			require.NoError(t, os.Remove(filepath.Join(repo, "go.mod")))
			require.NoError(t, os.Remove(filepath.Join(repo, "source.go")))

			for _, directory := range []string{"app", "lib", "gone"} {
				require.NoError(t, os.MkdirAll(filepath.Join(repo, directory), 0o750))
			}

			appMod := "module example.com/app\n\ngo 1.27.1\nrequire example.com/lib v0.0.0\n"
			if workspace {
				write(t, repo, "go.work", "go 1.27.1\nuse (\n ./app\n ./lib\n)\n")
			} else {
				appMod += "replace example.com/lib => ../lib\n"
			}

			write(t, repo, "app/go.mod", appMod)
			write(t, repo, "app/source.go", "package app\nimport \"example.com/lib\"\nfunc Caller(){lib.Changed()}\n")
			write(t, repo, "lib/go.mod", "module example.com/lib\n\ngo 1.27.1\n")
			write(t, repo, "lib/source.go", "package lib\nfunc Changed(){}\nfunc Unrelated(){}\n")
			write(t, repo, "gone/go.mod", "module example.com/gone\n\ngo 1.27.1\n")
			write(t, repo, "gone/source.go", "package gone\nfunc RemovedModule(){}\n")
			git(t, repo, "add", "--all")
			git(t, repo, "commit", "--no-gpg-sign", "-m", "multiple modules")
			write(t, repo, "lib/source.go", "package lib\nfunc Changed(){Added()}\nfunc Added(){}\nfunc Unrelated(){}\n")
			require.NoError(t, os.RemoveAll(filepath.Join(repo, "gone")))

			git(t, repo, "add", "--all")
			require.NoError(t, os.MkdirAll(filepath.Join(repo, "new"), 0o750))

			write(t, repo, "new/go.mod", "module example.com/new\n\ngo 1.27.1\n")
			write(t, repo, "new/source.go", "package added\nfunc UntrackedModule(){}\n")
			originalStatus := commandOutput(t, repo, "git", "status", "--porcelain=v1", "--untracked-files=all")
			index, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			require.NoError(t, err)

			for _, args := range [][]string{{"godelta"}, {"godelta", "--staged"}} {
				var stdout, stderr bytes.Buffer
				// Invoking inside a nested module still reviews the full repository.
				require.NoError(t, testCommand(filepath.Join(repo, "app"), &stdout, &stderr).Run(t.Context(), args))

				text := stdout.String()
				assert.Contains(t, text, "app.Caller #183; unchanged")
				assert.Contains(t, text, "lib.Changed #183; modified")
				assert.Contains(t, text, "lib.Added #183; added")
				assert.Contains(t, text, "gone.RemovedModule #183; removed")
				assert.NotContains(t, text, "Unrelated")

				if len(args) == 1 {
					assert.Contains(t, text, "new.UntrackedModule #183; added")
				} else {
					assert.NotContains(t, text, "UntrackedModule")
				}

				assert.Contains(t, text, "-->|retained|")
				assert.NotContains(t, stderr.String(), "unresolved")
			}

			assert.Equal(t, originalStatus, commandOutput(t, repo, "git", "status", "--porcelain=v1", "--untracked-files=all"))

			afterIndex, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
			require.NoError(t, err)
			assert.Equal(t, index, afterIndex)
		})
	}
}
