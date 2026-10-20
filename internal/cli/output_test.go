package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/graph"
)

func TestFormatParsing(t *testing.T) {
	t.Parallel()

	for _, command := range [][]string{nil, {"pr", "123"}} {
		for _, value := range []format{formatAuto, formatMermaid, formatTerminal} {
			args := append(append([]string(nil), command...), "--format", string(value))

			t.Run(strings.Join(args, " "), func(t *testing.T) {
				t.Parallel()

				var stderr bytes.Buffer
				request, err := parseRequest(args, &stderr)
				require.NoError(t, err)
				assert.Equal(t, value, request.format)
			})
		}
	}

	for _, args := range [][]string{{"--format", "json"}, {"--format"}, {"pr", "123", "--format", "json"}, {"pr", "123", "--format"}} {
		_, err := parseRequest(args, io.Discard)
		require.ErrorIs(t, err, ErrUsage)
	}
}

func TestRenderFormatsOnPipe(t *testing.T) {
	t.Parallel()

	for _, requested := range []format{formatAuto, formatMermaid, formatTerminal} {
		var output bytes.Buffer
		require.NoError(t, render(&output, graph.Graph{}, requested))

		if requested == formatTerminal {
			assert.Contains(t, output.String(), "No function changes")
			assert.Contains(t, output.String(), "┌")
		} else {
			assert.True(t, strings.HasPrefix(output.String(), "flowchart LR\n"))
		}

		assert.NotContains(t, output.String(), "\x1b")
	}
}

func TestRegularFilesAreNotTerminals(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "graph")
	require.NoError(t, err)

	t.Cleanup(func() { require.NoError(t, file.Close()) })
	assert.False(t, isTerminal(file))
	require.NoError(t, render(file, graph.Graph{}, formatAuto))

	_, err = file.Seek(0, io.SeekStart)
	require.NoError(t, err)

	data, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(data), "flowchart LR\n"))
}
