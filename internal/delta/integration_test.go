package delta_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
)

func TestCompareLoadedModules(t *testing.T) {
	t.Parallel()

	before := loadArchive(t, "before.txtar")
	after := loadArchive(t, "after.txtar")
	got := delta.Compare(before, after)
	statuses := make(map[analysis.ID]delta.FunctionStatus)
	for _, change := range got.Functions {
		statuses[change.ID] = change.Status
	}

	assert.Equal(
		t,
		map[analysis.ID]delta.FunctionStatus{
			".:Added":                delta.Added,
			".:Changed":              delta.Modified,
			".:Closures":             delta.Modified,
			".:Closures.anonymous.1": delta.Modified,
			".:Entry":                delta.Modified,
			".:Generic":              delta.Unchanged,
			".:Moved":                delta.Unchanged,
			".:RenamedNew":           delta.Added,
			".:RenamedOld":           delta.Removed,
			".:Removed":              delta.Removed,
			".:Unchanged":            delta.Unchanged,
			".:Worker.Run":           delta.Modified,
			".:functions.go.init.1":  delta.Modified,
			"helper:Caller":          delta.Unchanged,
			"helper:Kept":            delta.Modified,
		},
		statuses,
	)

	calls := make(map[analysis.Call]delta.CallStatus)
	for _, change := range got.Calls {
		calls[change.Call] = change.Status
	}

	assert.Equal(
		t,
		map[analysis.Call]delta.CallStatus{
			{Caller: ".:Added", Callee: ".:Added"}:                   delta.CallAdded,
			{Caller: ".:Closures", Callee: ".:Closures.anonymous.1"}: delta.CallRetained,
			{Caller: ".:Closures.anonymous.1", Callee: ".:Added"}:    delta.CallAdded,
			{Caller: ".:Closures.anonymous.1", Callee: ".:Removed"}:  delta.CallRemoved,
			{Caller: ".:Entry", Callee: ".:Added"}:                   delta.CallAdded,
			{Caller: ".:Entry", Callee: ".:Changed"}:                 delta.CallRetained,
			{Caller: ".:Entry", Callee: ".:Generic"}:                 delta.CallRetained,
			{Caller: ".:Entry", Callee: ".:Removed"}:                 delta.CallRemoved,
			{Caller: ".:Entry", Callee: ".:Worker.Run"}:              delta.CallRetained,
			{Caller: ".:Entry", Callee: "helper:Kept"}:               delta.CallRetained,
			{Caller: ".:Removed", Callee: ".:Removed"}:               delta.CallRemoved,
			{Caller: ".:functions.go.init.1", Callee: ".:Added"}:     delta.CallAdded,
			{Caller: ".:functions.go.init.1", Callee: ".:Removed"}:   delta.CallRemoved,
			{Caller: "helper:Caller", Callee: "helper:Kept"}:         delta.CallRetained,
		},
		calls,
	)

	for _, change := range got.Functions {
		if change.ID == ".:Moved" {
			require.NotNil(t, change.Before)
			require.NotNil(t, change.After)
			assert.Equal(t, "stable.go", change.Before.File)
			assert.Equal(t, "moved.go", change.After.File)
		}
	}
}

func TestCompareIgnoresGlobalChanges(t *testing.T) {
	t.Parallel()

	var snapshots []analysis.Snapshot
	for _, declarations := range []string{
		"const Value = 1\nvar Global = 1\ntype Payload struct{ Value int }\n",
		"const Value = 2\nvar Global = 2\ntype Payload struct{ Value string }\n",
	} {
		dir := t.TempDir()
		writeSource(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.27.1\n")
		writeSource(t, dir, "source.go", "package fixture\n"+declarations+"func Kept() int { return Value }\n")
		snapshot, err := analysis.Load(t.Context(), dir)
		require.NoError(t, err)

		snapshots = append(snapshots, snapshot)
	}

	got := delta.Compare(snapshots[0], snapshots[1])
	require.Len(t, got.Functions, 1)
	assert.Equal(t, delta.Unchanged, got.Functions[0].Status)
	assert.Empty(t, got.Calls)
}

func loadArchive(t *testing.T, name string) analysis.Snapshot {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	dir := t.TempDir()
	for _, file := range txtar.Parse(source).Files {
		writeSource(t, dir, file.Name, string(file.Data))
	}

	snapshot, err := analysis.Load(t.Context(), dir)
	require.NoError(t, err)

	return snapshot
}

func writeSource(t *testing.T, dir, name, content string) {
	t.Helper()

	filename := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(filename), 0o750))
	require.NoError(t, os.WriteFile(filename, []byte(content), 0o600))
}
