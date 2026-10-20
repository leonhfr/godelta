package cli_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/leonhfr/godelta/internal/cli"
)

func TestVersionOutsideRepository(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"dev", "v0.1.0", "v0.2.0-rc.1"} {
		var stdout, stderr bytes.Buffer
		require.NoError(t, cli.Run(t.Context(), t.TempDir(), []string{"--version"}, &stdout, &stderr, version))
		assert.Equal(t, "godelta version "+version+"\n", stdout.String())
		assert.Empty(t, stderr.String())
	}

	require.ErrorIs(t, cli.Run(t.Context(), t.TempDir(), []string{"--version"}, failingWriter{}, io.Discard, "dev"), io.ErrClosedPipe)
}
