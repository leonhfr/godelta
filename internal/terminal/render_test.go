package terminal_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/terminal"
)

func TestRenderGraph(t *testing.T) {
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
				call(
					"Caller",
					"Changed",
					delta.CallRetained,
				),
				call("Changed", "Added", delta.CallAdded),
				call("Changed", "Removed", delta.CallRemoved),
			},
		},
		{Functions: []delta.FunctionChange{function("Alone", "孤立", "pkg-odd.v1/extra.go", delta.Removed)}},
	}}
	var output bytes.Buffer
	require.NoError(t, terminal.Render(&output, review, terminal.Options{}))

	for _, label := range []string{"Change group 1", "Change group 2", "Caller · unchanged", "(*Worker).Run · modified", "helpers.Added · added", "helpers.OldHelper · removed", "pkg-odd.v1.孤立 · removed", "added", "removed", "retained", "┌", "─", "│"} {
		assert.Contains(t, output.String(), label)
	}

	assert.NotContains(t, output.String(), "#183;")
	assert.NotContains(t, output.String(), "linkStyle")
	assert.NotContains(t, output.String(), "-. removed .->")

	var repeated bytes.Buffer
	require.NoError(t, terminal.Render(&repeated, review, terminal.Options{}))
	assert.Equal(t, output.String(), repeated.String())
}

func TestRenderSharedTargetsAndCycles(t *testing.T) {
	t.Parallel()

	review := graph.Graph{Components: []graph.Component{{
		Functions: []delta.FunctionChange{
			function(
				"A",
				"A",
				"source.go",
				delta.Modified,
			),
			function("B", "B", "source.go", delta.Unchanged),
			function("C", "C", "source.go", delta.Unchanged),
		},
		Calls: []delta.CallChange{
			call("A", "A", delta.CallRetained),
			call("A", "B", delta.CallAdded),
			call("A", "C", delta.CallRetained),
			call("B", "C", delta.CallRemoved),
			call("C", "A", delta.CallRetained),
		},
	}}}
	var output bytes.Buffer
	require.NoError(t, terminal.Render(&output, review, terminal.Options{}))

	for _, name := range []string{"A · modified", "B · unchanged", "C · unchanged", "added", "removed", "retained"} {
		assert.Contains(t, output.String(), name)
	}

	assert.Equal(t, 1, strings.Count(output.String(), "A · modified"))
	assert.Equal(t, 1, strings.Count(output.String(), "B · unchanged"))
	assert.Equal(t, 1, strings.Count(output.String(), "C · unchanged"))
}

func TestRenderRootsAfterContextTruncation(t *testing.T) {
	t.Parallel()
	// Context truncation can leave several unconnected islands in one component.
	review := graph.Graph{Components: []graph.Component{{
		Functions: []delta.FunctionChange{
			function("A", "A", "source.go", delta.Added),
			function("B", "B", "source.go", delta.Added),
			function("C", "C", "source.go", delta.Modified),
			function("D", "D", "source.go", delta.Modified),
		},
		Calls: []delta.CallChange{
			call("C", "D", delta.CallRetained),
			call("D", "C", delta.CallRetained),
		},
	}}}
	var output bytes.Buffer
	require.NoError(t, terminal.Render(&output, review, terminal.Options{}))

	for _, name := range []string{"A", "B", "C", "D"} {
		assert.Contains(t, output.String(), name+" ·")
	}
}

func TestRenderWideAndLarge(t *testing.T) {
	t.Parallel()

	var component graph.Component
	for index := range 12 {
		name := fmt.Sprintf("Function%02d", index) + strings.Repeat("界", 20)
		id := analysis.ID(name)
		component.Functions = append(component.Functions, function(id, name, "source.go", delta.Added))
		if index > 0 {
			component.Calls = append(component.Calls, call(component.Functions[index-1].ID, id, delta.CallAdded))
		}
	}

	var output bytes.Buffer
	require.NoError(t, terminal.Render(&output, graph.Graph{Components: []graph.Component{component}}, terminal.Options{}))

	for _, fn := range component.Functions {
		assert.Contains(t, output.String(), fn.After.Name)
	}
}

func TestRenderColorsAndControlEscapes(t *testing.T) {
	t.Parallel()

	review := graph.Graph{Components: []graph.Component{{
		Functions: []delta.FunctionChange{
			function("Added", "A\n\x1b[31m", "pkg\t/source.go", delta.Added),
			function("Removed", "Removed", "source.go", delta.Removed),
			function("Modified", "Modified", "source.go", delta.Modified),
			function("Unchanged", "Unchanged", "source.go", delta.Unchanged),
		},
		Calls: []delta.CallChange{call("Added", "Removed", delta.CallRemoved)},
	}}}
	for _, color := range []bool{true, false} {
		var output bytes.Buffer
		require.NoError(t, terminal.Render(&output, review, terminal.Options{Color: color}))
		assert.Contains(t, output.String(), `pkg\x09.A\x0a\x1b[31m · added`)
		assert.NotContains(t, output.String(), "A\n\x1b[31m")

		if color {
			for _, code := range []string{"32", "31", "90", "94"} {
				assert.Contains(t, output.String(), "\x1b["+code+"m")
			}
		} else {
			assert.NotContains(t, output.String(), "\x1b")
		}
	}
}

func TestRenderEmptyAndWriterFailures(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	require.NoError(t, terminal.Render(&output, graph.Graph{}, terminal.Options{Color: true}))
	assert.Contains(t, output.String(), "No function changes")
	assert.Contains(t, output.String(), "┌")
	require.ErrorIs(t, terminal.Render(failingWriter{}, graph.Graph{}, terminal.Options{}), io.ErrClosedPipe)
}

func function(id analysis.ID, name, file string, status delta.FunctionStatus) delta.FunctionChange {
	source := &analysis.Function{ID: id, Name: name, File: file}
	fn := delta.FunctionChange{ID: id, Status: status, After: source}
	if status == delta.Removed {
		fn.Before = source
		fn.After = nil
	}

	return fn
}

func call(caller, callee analysis.ID, status delta.CallStatus) delta.CallChange {
	return delta.CallChange{Call: analysis.Call{Caller: caller, Callee: callee}, Status: status}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
