package cli

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePR(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"123"},
		{"123", "--repo", "owner/project"},
		{"--repo", "owner/project", "123"},
		{"123", "-R", "owner/project"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			options, err := parsePR(args, &stderr)
			require.NoError(t, err)
			assert.Equal(t, 123, options.number)

			if len(args) > 1 {
				assert.Equal(t, "owner/project", options.repository)
			}

			assert.Empty(t, stderr.String())
		})
	}

	for _, args := range [][]string{{}, {"0"}, {"-1"}, {"abc"}, {"123", "456"}, {"123", "--staged"}, {"123", "--repo"}, {"999999999999999999999999999"}} {
		var stderr bytes.Buffer
		_, err := parsePR(args, &stderr)
		require.ErrorIs(t, err, ErrUsage)
	}

	var stderr bytes.Buffer
	_, err := parsePR([]string{"--help"}, &stderr)
	require.ErrorIs(t, err, flag.ErrHelp)
	assert.Contains(t, stderr.String(), "repo")

	cause := errors.New("write failed")
	_, err = parsePR([]string{"--help"}, failedPRWriter{err: cause})
	require.ErrorIs(t, err, cause)
}

type failedPRWriter struct{ err error }

func (writer failedPRWriter) Write([]byte) (int, error) { return 0, writer.err }
