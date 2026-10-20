package analysis_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/txtar"

	"github.com/leonhfr/godelta/internal/analysis"
)

func TestLoadInventory(t *testing.T) {
	t.Parallel()

	snapshot, err := analysis.Load(t.Context(), fixtureModule(t))
	require.NoError(t, err)

	want := []analysis.ID{
		".:Box.Use",
		".:Closures",
		".:Closures.anonymous.1",
		".:Closures.anonymous.1.anonymous.1",
		".:Closures.anonymous.2",
		".:Entry",
		".:Generic",
		".:Leaf",
		".:Worker.Run",
		".:functions.go.init.1",
		".:functions.go.init.2",
		".:other.go.init.1",
		"helper:Call",
		"helper:UnchangedCaller",
	}
	got := slices.Sorted(maps.Keys(snapshot.Functions))
	assert.Equal(t, want, got)

	method := snapshot.Functions[".:Worker.Run"]
	assert.Equal(t, "(*Worker).Run", method.Name)
	assert.Equal(t, "functions.go", method.File)
	assert.Positive(t, method.Line)
	assert.Equal(t, "example.com/review", method.Package)
}

func TestLoadCalls(t *testing.T) {
	t.Parallel()

	snapshot, err := analysis.Load(t.Context(), fixtureModule(t))
	require.NoError(t, err)

	want := []analysis.Call{
		{Caller: ".:Box.Use", Callee: ".:Leaf"},
		{Caller: ".:Closures", Callee: ".:Closures.anonymous.1"},
		{Caller: ".:Closures.anonymous.1", Callee: ".:Closures.anonymous.1.anonymous.1"},
		{Caller: ".:Closures.anonymous.1", Callee: ".:Leaf"},
		{Caller: ".:Closures.anonymous.1.anonymous.1", Callee: "helper:Call"},
		{Caller: ".:Closures.anonymous.2", Callee: "helper:Call"},
		{Caller: ".:Entry", Callee: ".:Box.Use"},
		{Caller: ".:Entry", Callee: ".:Generic"},
		{Caller: ".:Entry", Callee: ".:Leaf"},
		{Caller: ".:Entry", Callee: ".:Worker.Run"},
		{Caller: ".:Entry", Callee: "helper:Call"},
		{Caller: ".:Worker.Run", Callee: ".:Leaf"},
		{Caller: ".:functions.go.init.1", Callee: ".:Leaf"},
		{Caller: ".:functions.go.init.2", Callee: "helper:Call"},
		{Caller: ".:other.go.init.1", Callee: ".:Leaf"},
		{Caller: "helper:UnchangedCaller", Callee: "helper:Call"},
	}
	assert.Equal(t, want, snapshot.Calls)

	for id, fn := range snapshot.Functions {
		expected := 0
		switch id {
		case ".:Entry":
			expected = 2

		case ".:Closures":
			expected = 1
		}

		assert.Equal(t, expected, fn.DynamicCalls, "%s unresolved calls", id)
	}
}

func TestLoadFingerprint(t *testing.T) {
	t.Parallel()

	versions := []struct {
		name   string
		file   string
		source string
		equal  bool
	}{
		{name: "original", file: "first.go", source: "func Save() int { return 1 }", equal: true},
		{name: "formatting and comments", file: "moved.go", source: "// Save documents.\nfunc Save( ) int {\n/* note */ return 1\n}\n", equal: true},
		{name: "body", file: "first.go", source: "func Save() int { return 2 }"},
		{name: "signature", file: "first.go", source: "func Save() int64 { return 1 }"},
	}
	var baseline analysis.Function
	for index, version := range versions {
		dir := writeModule(t, map[string]string{version.file: "package fixture\n" + version.source})
		snapshot, err := analysis.Load(t.Context(), dir)
		require.NoError(t, err)

		fn := snapshot.Functions[".:Save"]
		if index == 0 {
			baseline = fn
		}

		if version.equal {
			assert.Equal(t, baseline.Fingerprint, fn.Fingerprint, version.name)
		} else {
			assert.NotEqual(t, baseline.Fingerprint, fn.Fingerprint, version.name)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  packages.ErrorKind
	}{
		{name: "type error", files: map[string]string{"broken.go": "package fixture\nfunc Broken() { missing() }"}, want: packages.TypeError},
		{name: "syntax error", files: map[string]string{"broken.go": "package fixture\nfunc Broken("}, want: packages.ParseError},
		{
			name:  "dependency error",
			files: map[string]string{"broken.go": "package fixture\nimport _ \"example.invalid/missing\""},
			want:  packages.ListError,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, test.files)
			_, err := analysis.Load(t.Context(), dir)
			require.ErrorIs(t, err, analysis.ErrLoadPackages)

			var diagnostics *analysis.PackageErrors
			require.ErrorAs(t, err, &diagnostics)

			kinds := make([]packages.ErrorKind, 0, len(diagnostics.Diagnostics))
			for _, diagnostic := range diagnostics.Diagnostics {
				kinds = append(kinds, diagnostic.Kind)
			}

			assert.Contains(t, kinds, test.want)

			var original packages.Error
			require.ErrorAs(t, err, &original)
			assert.Contains(t, diagnostics.Diagnostics, original)
		})
	}
}

func TestLoadRequiresModuleRoot(t *testing.T) {
	t.Parallel()

	_, err := analysis.Load(t.Context(), t.TempDir())
	require.ErrorIs(t, err, analysis.ErrInvalidModule)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestLoadSnapshotMatching(t *testing.T) {
	t.Parallel()

	before := writeModule(t, map[string]string{
		"old.go": `package fixture
const Value = 1
type Worker struct{ Field int }
func (*Worker) Run() { Target() }
func Target() {}
func Removed() {}
`,
		"caller/caller.go": `package caller
import "example.com/fixture"
func Unchanged() { fixture.Target() }
`,
	})
	after := writeModule(t, map[string]string{
		"moved.go": `package fixture
const Value = 2
type Worker struct{ Field string }
func (Worker) Run() { Target() }
func Target() {}
func Added() {}
`,
		"caller/caller.go": `package caller
import "example.com/fixture"
func Unchanged() { fixture.Target() }
`,
	})
	oldSnapshot, err := analysis.Load(t.Context(), before)
	require.NoError(t, err)

	newSnapshot, err := analysis.Load(t.Context(), after)
	require.NoError(t, err)

	for _, id := range []analysis.ID{".:Target", "caller:Unchanged"} {
		assert.Equal(
			t,
			oldSnapshot.Functions[id].Fingerprint,
			newSnapshot.Functions[id].Fingerprint,
			"%s changed after a file move or global/type edit",
			id,
		)
	}

	assert.NotEqual(
		t,
		oldSnapshot.Functions[".:Worker.Run"].Fingerprint,
		newSnapshot.Functions[".:Worker.Run"].Fingerprint,
		"pointer-to-value receiver edit must change the fingerprint",
	)
	assert.NotContains(t, newSnapshot.Functions, analysis.ID(".:Removed"))
	assert.NotContains(t, oldSnapshot.Functions, analysis.ID(".:Added"))
	assert.Equal(t, oldSnapshot.Calls, newSnapshot.Calls, "moving files and editing globals changed call relationships")
}

func TestLoadOrdinalInsertion(t *testing.T) {
	t.Parallel()

	before := writeModule(t, map[string]string{"functions.go": `package fixture
func Parent() { func() { Target() }() }
func Target() {}
func init() { Target() }
`})
	after := writeModule(t, map[string]string{"functions.go": `package fixture
func Parent() { func() {}(); func() { Target() }() }
func Target() {}
func init() {}
func init() { Target() }
`})
	oldSnapshot, err := analysis.Load(t.Context(), before)
	require.NoError(t, err)

	newSnapshot, err := analysis.Load(t.Context(), after)
	require.NoError(t, err)

	for _, id := range []analysis.ID{".:Parent", ".:Parent.anonymous.1", ".:functions.go.init.1"} {
		assert.NotEqual(
			t,
			oldSnapshot.Functions[id].Fingerprint,
			newSnapshot.Functions[id].Fingerprint,
			"%s fingerprint unchanged after inserting an earlier ordinal",
			id,
		)
	}

	for _, id := range []analysis.ID{".:Parent.anonymous.2", ".:functions.go.init.2"} {
		assert.Contains(t, newSnapshot.Functions, id)
	}
}

func TestLoadSignificantSemicolon(t *testing.T) {
	t.Parallel()

	hashes := make(map[[32]byte]struct{})
	for _, condition := range []string{"value()", "value();"} {
		dir := writeModule(t, map[string]string{"switch.go": "package fixture\n" +
			"func Save() bool { value := func() bool { return false }; switch " + condition +
			" { case true: return true; default: return false } }"})
		snapshot, err := analysis.Load(t.Context(), dir)
		require.NoError(t, err)

		hashes[snapshot.Functions[".:Save"].Fingerprint] = struct{}{}
	}

	assert.Len(t, hashes, 2, "a switch expression and an initializer must have different fingerprints")
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.27.1\n")
	for name, content := range files {
		writeFile(t, dir, name, content)
	}

	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	filename := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o750))

	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
}

func fixtureModule(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile("testdata/module.txtar")
	require.NoError(t, err)

	dir := t.TempDir()
	for _, file := range txtar.Parse(source).Files {
		writeFile(t, dir, file.Name, string(file.Data))
	}

	return dir
}
