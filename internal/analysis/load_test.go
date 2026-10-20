package analysis_test

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"github.com/leonhfr/godelta/internal/analysis"
)

func TestLoadInventory(t *testing.T) {
	t.Parallel()

	snapshot, err := analysis.Load(t.Context(), fixtureModule(t))
	if err != nil {
		t.Fatal(err)
	}

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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("functions = %v, want %v", got, want)
	}

	method := snapshot.Functions[".:Worker.Run"]
	if method.Name != "(*Worker).Run" || method.File != "functions.go" || method.Line < 1 {
		t.Errorf("method location/name = %+v", method)
	}

	if method.Package != "example.com/review" {
		t.Errorf("package = %q", method.Package)
	}
}

func TestLoadCalls(t *testing.T) {
	t.Parallel()

	snapshot, err := analysis.Load(t.Context(), fixtureModule(t))
	if err != nil {
		t.Fatal(err)
	}

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
	if !reflect.DeepEqual(snapshot.Calls, want) {
		t.Fatalf("calls =\n%+v\nwant\n%+v", snapshot.Calls, want)
	}

	for id, fn := range snapshot.Functions {
		expected := 0
		switch id {
		case ".:Entry":
			expected = 2

		case ".:Closures":
			expected = 1
		}

		if fn.DynamicCalls != expected {
			t.Errorf("%s unresolved calls = %d, want %d", id, fn.DynamicCalls, expected)
		}
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
		if err != nil {
			t.Fatal(err)
		}

		fn := snapshot.Functions[".:Save"]
		if index == 0 {
			baseline = fn
		}

		if equal := fn.Fingerprint == baseline.Fingerprint; equal != version.equal {
			t.Errorf("%s fingerprint equality = %t, want %t", version.name, equal, version.equal)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "type error", files: map[string]string{"broken.go": "package fixture\nfunc Broken() { missing() }"}, want: "undefined: missing"},
		{name: "syntax error", files: map[string]string{"broken.go": "package fixture\nfunc Broken("}, want: "load Go packages"},
		{
			name:  "dependency error",
			files: map[string]string{"broken.go": "package fixture\nimport _ \"example.invalid/missing\""},
			want:  "example.invalid/missing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			dir := writeModule(t, test.files)
			_, err := analysis.Load(t.Context(), dir)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadRequiresModuleRoot(t *testing.T) {
	t.Parallel()

	_, err := analysis.Load(t.Context(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "go.mod") {
		t.Fatalf("error = %v, want missing module error", err)
	}
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
	if err != nil {
		t.Fatal(err)
	}

	newSnapshot, err := analysis.Load(t.Context(), after)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []analysis.ID{".:Target", "caller:Unchanged"} {
		if oldSnapshot.Functions[id].Fingerprint != newSnapshot.Functions[id].Fingerprint {
			t.Errorf("%s changed after a file move or global/type edit", id)
		}
	}

	if oldSnapshot.Functions[".:Worker.Run"].Fingerprint == newSnapshot.Functions[".:Worker.Run"].Fingerprint {
		t.Error("pointer-to-value receiver edit must change the fingerprint")
	}

	if _, exists := newSnapshot.Functions[".:Removed"]; exists {
		t.Error("removed function remains in new snapshot")
	}

	if _, exists := oldSnapshot.Functions[".:Added"]; exists {
		t.Error("added function exists in old snapshot")
	}

	if !reflect.DeepEqual(oldSnapshot.Calls, newSnapshot.Calls) {
		t.Errorf("moving files and editing globals changed call relationships: %v -> %v", oldSnapshot.Calls, newSnapshot.Calls)
	}
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
	if err != nil {
		t.Fatal(err)
	}

	newSnapshot, err := analysis.Load(t.Context(), after)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []analysis.ID{".:Parent", ".:Parent.anonymous.1", ".:functions.go.init.1"} {
		if oldSnapshot.Functions[id].Fingerprint == newSnapshot.Functions[id].Fingerprint {
			t.Errorf("%s fingerprint unchanged after inserting an earlier ordinal", id)
		}
	}

	for _, id := range []analysis.ID{".:Parent.anonymous.2", ".:functions.go.init.2"} {
		if _, exists := newSnapshot.Functions[id]; !exists {
			t.Errorf("missing inserted ordinal %s", id)
		}
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
		if err != nil {
			t.Fatal(err)
		}

		hashes[snapshot.Functions[".:Save"].Fingerprint] = struct{}{}
	}

	if len(hashes) != 2 {
		t.Fatal("a switch expression and an initializer must have different fingerprints")
	}
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
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixtureModule(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile("testdata/module.txtar")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	for _, file := range txtar.Parse(source).Files {
		writeFile(t, dir, file.Name, string(file.Data))
	}

	return dir
}
