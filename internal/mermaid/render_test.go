package mermaid_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/mermaid"
)

func TestRenderGolden(t *testing.T) {
	t.Parallel()

	review := graph.Graph{Components: []graph.Component{
		{
			Functions: []delta.FunctionChange{
				function("Added", "Added", "helpers/source.go", delta.Added),
				function("Caller", "Caller", "source.go", delta.Unchanged),
				function("Changed", "(*Worker).Run", "source.go", delta.Modified),
				function("Removed", "OldHelper", "helpers/old.go", delta.Removed),
			},
			Calls: []delta.CallChange{
				{Call: analysis.Call{Caller: "Caller", Callee: "Changed"}, Status: delta.CallRetained},
				{Call: analysis.Call{Caller: "Changed", Callee: "Added"}, Status: delta.CallAdded},
				{Call: analysis.Call{Caller: "Changed", Callee: "Removed"}, Status: delta.CallRemoved},
			},
		},
		{Functions: []delta.FunctionChange{function("Alone", "孤立", "pkg-odd.v1/extra.go", delta.Removed)}},
	}}
	var output bytes.Buffer
	require.NoError(t, mermaid.Render(&output, review))

	want, err := os.ReadFile("testdata/review.mmd")
	require.NoError(t, err)
	assert.Equal(t, string(want), output.String())
}

func TestRenderEscapesLabels(t *testing.T) {
	t.Parallel()

	review := graph.Graph{Components: []graph.Component{{Functions: []delta.FunctionChange{
		function("unsafe ID ]\nend", "审查\"[]<script>&#34;|`\\\n", "source.go", delta.Added),
	}}}}
	var output bytes.Buffer
	require.NoError(t, mermaid.Render(&output, review))
	assert.Contains(t, output.String(), "审查#34;#91;#93;#60;script#62;#38;#35;34#59;#124;#96;#92;#10;")
	assert.NotContains(t, output.String(), "<script>")
	assert.NotContains(t, output.String(), "unsafe ID")
}

func TestRenderEmpty(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	require.NoError(t, mermaid.Render(&output, graph.Graph{}))
	assert.Equal(t, "flowchart LR\n    no_changes[\"No function changes\"]\n", output.String())
}

func TestRenderWriteFailure(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, mermaid.Render(failingWriter{}, graph.Graph{}), io.ErrClosedPipe)
}

func function(id analysis.ID, name, file string, status delta.FunctionStatus) delta.FunctionChange {
	source := analysis.Function{ID: id, Name: name, File: file}
	change := delta.FunctionChange{ID: id, Status: status}
	if status != delta.Removed {
		change.After = &source
	}

	if status != delta.Added {
		before := source
		change.Before = &before
	}

	return change
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}
