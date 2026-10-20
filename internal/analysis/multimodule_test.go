package analysis_test

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
)

func TestLoadMultipleModules(t *testing.T) {
	t.Parallel()

	for _, workspace := range []bool{false, true} {
		name := "replacements"
		if workspace {
			name = "workspace"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			appMod := "module example.com/app\n\ngo 1.27.1\nrequire example.com/lib v0.0.0\n"
			if workspace {
				writeFile(t, root, "go.work", "go 1.27.1\nuse (\n ./app\n ./lib\n)\n")
			} else {
				appMod += "replace example.com/lib => ../lib\n"
			}

			writeFile(t, root, "app/go.mod", appMod)
			writeFile(t, root, "app/app.go", `package app
import "example.com/lib"
func Caller(){lib.Leaf(); new(lib.Worker[int]).Run(); lib.Generic(1)}
`)
			writeFile(t, root, "lib/go.mod", "module example.com/lib\n\ngo 1.27.1\n")
			writeFile(t, root, "lib/lib.go", `package lib
func Leaf(){}
type Worker[T any] struct{}
func (*Worker[T]) Run(){Leaf()}
func Generic[T any](value T){Leaf()}
`)
			// Unlisted modules remain visible, without inventing a workspace relationship.
			writeFile(t, root, "independent/go.mod", "module example.com/independent\n\ngo 1.27.1\n")
			writeFile(t, root, "independent/lib.go", "package independent\nfunc Alone(){}\n")
			snapshot, err := analysis.Load(t.Context(), root)
			require.NoError(t, err)
			assert.Len(t, snapshot.Functions, 5)
			assert.Equal(t, "app/app.go", snapshot.Functions["app:Caller"].File)
			assert.Equal(t, "example.com/lib", snapshot.Functions["lib:Leaf"].Package)
			assert.Contains(t, snapshot.Functions, analysis.ID("independent:Alone"))
			assert.Equal(
				t,
				[]analysis.Call{
					{Caller: "app:Caller", Callee: "lib:Generic"},
					{Caller: "app:Caller", Callee: "lib:Leaf"},
					{Caller: "app:Caller", Callee: "lib:Worker.Run"},
					{Caller: "lib:Generic", Callee: "lib:Leaf"},
					{Caller: "lib:Worker.Run", Callee: "lib:Leaf"},
				},
				snapshot.Calls,
			)

			repeated, err := analysis.Load(t.Context(), root)
			require.NoError(t, err)
			assert.Equal(t, snapshot, repeated)
		})
	}
}

func TestLoadSameModulePathInIndependentDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, directory := range []string{"a", "b"} {
		writeFile(t, root, directory+"/go.mod", "module example.com/same\n\ngo 1.27.1\n")
		writeFile(t, root, directory+"/source.go", "package same\nfunc Caller(){Leaf()}\nfunc Leaf(){}\n")
	}

	snapshot, err := analysis.Load(t.Context(), root)
	require.NoError(t, err)
	assert.Len(t, snapshot.Functions, 4)
	assert.Equal(t, []analysis.Call{{Caller: "a:Caller", Callee: "a:Leaf"}, {Caller: "b:Caller", Callee: "b:Leaf"}}, snapshot.Calls)
}

//nolint:paralleltest // Sets process-wide workspace discovery to an unrelated path.
func TestLoadIgnoresAmbientWorkspace(t *testing.T) {
	t.Setenv("GOWORK", t.TempDir()+"/missing.work")
	root := writeModule(t, map[string]string{"source.go": "package fixture\nfunc Leaf(){}\n"})
	snapshot, err := analysis.Load(t.Context(), root)
	require.NoError(t, err)
	assert.Contains(t, snapshot.Functions, analysis.ID(".:Leaf"))
}

//nolint:paralleltest // Selects a build flag through the process-wide Go environment.
func TestLoadLocalReplacementWithTrimpath(t *testing.T) {
	t.Setenv("GOFLAGS", "-trimpath")
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/app\n\ngo 1.27.1\nrequire example.com/alias v0.0.0\nreplace example.com/alias => ./lib\n")
	writeFile(t, root, "app.go", "package app\nimport \"example.com/alias\"\nfunc Caller(){lib.Leaf()}\n")
	writeFile(t, root, "lib/go.mod", "module example.com/lib\n\ngo 1.27.1\n")
	writeFile(t, root, "lib/lib.go", "package lib\nfunc Leaf(){}\n")
	snapshot, err := analysis.Load(t.Context(), root)
	require.NoError(t, err)
	assert.Equal(t, []analysis.Call{{Caller: ".:Caller", Callee: "lib:Leaf"}}, snapshot.Calls)
}

//nolint:paralleltest // Uses an isolated module cache and local file proxy through Go environment variables.
func TestLoadDownloadedModuleIsNotLocal(t *testing.T) {
	root := t.TempDir()
	proxy := t.TempDir()
	cache := t.TempDir()
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", cache)
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOFLAGS", "-trimpath -modcacherw")
	t.Setenv("GOWORK", "off")
	module := "module example.com/lib\n\ngo 1.27.1\n"
	writeFile(t, proxy, "example.com/lib/@v/v1.0.0.mod", module)
	writeFile(t, proxy, "example.com/lib/@v/v1.0.0.info", `{"Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`)
	file, err := os.Create(filepath.Join(proxy, "example.com/lib/@v/v1.0.0.zip"))
	require.NoError(t, err)

	archive := zip.NewWriter(file)
	for name, source := range map[string]string{"go.mod": module, "lib.go": "package lib\nfunc Leaf(){}\n"} {
		entry, err := archive.Create("example.com/lib@v1.0.0/" + name)
		require.NoError(t, err)

		_, err = entry.Write([]byte(source))
		require.NoError(t, err)
	}

	require.NoError(t, archive.Close())
	require.NoError(t, file.Close())

	writeFile(t, root, "app/go.mod", "module example.com/app\n\ngo 1.27.1\nrequire example.com/lib v1.0.0\n")
	writeFile(t, root, "app/app.go", "package app\nimport \"example.com/lib\"\nfunc Caller(){lib.Leaf()}\n")
	writeFile(t, root, "lib/go.mod", module)
	writeFile(t, root, "lib/lib.go", "package lib\nfunc Leaf(){}\n")
	command := exec.CommandContext(t.Context(), "go", "mod", "download", "example.com/lib")
	command.Dir = filepath.Join(root, "app")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)

	snapshot, err := analysis.Load(t.Context(), root)
	require.NoError(t, err)
	assert.Len(t, snapshot.Functions, 2)
	assert.Empty(t, snapshot.Calls, "the downloaded Leaf must not resolve to lib:Leaf")
}
