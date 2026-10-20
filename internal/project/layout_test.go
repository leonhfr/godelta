package project_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"

	"github.com/leonhfr/godelta/internal/project"
)

func TestConfigurationBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{name: "outside workspace", files: map[string]string{"go.work": "go 1.27.1\nuse ../outside\n"}, want: project.ErrUnsupportedLayout},
		{
			name:  "outside replacement",
			files: map[string]string{"go.mod": "module example.com/root\nreplace example.com/outside => ../outside\n"},
			want:  project.ErrUnsupportedLayout,
		},
		{name: "missing member", files: map[string]string{"go.work": "go 1.27.1\nuse ./missing\n"}, want: project.ErrInvalidModule},
		{
			name:  "missing replacement",
			files: map[string]string{"go.mod": "module example.com/root\nreplace example.com/missing => ./missing\n"},
			want:  project.ErrInvalidModule,
		},
		{name: "nested workspace", files: map[string]string{"nested/go.work": "go 1.27.1\nuse .\n"}, want: project.ErrUnsupportedLayout},
		{name: "invalid nested module", files: map[string]string{"nested/go.mod": "go 1.27.1\n"}, want: project.ErrInvalidModule},
		{name: "empty workspace", files: map[string]string{"go.work": "go 1.27.1\n"}, want: project.ErrInvalidModule},
		{
			name:  "outside workspace replacement",
			files: map[string]string{"go.work": "go 1.27.1\nuse .\nreplace example.com/outside => ../outside\n"},
			want:  project.ErrUnsupportedLayout,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write(t, root, "go.mod", "module example.com/root\n")
			for name, contents := range test.files {
				write(t, root, name, contents)
			}

			_, err := project.Discover(root)
			require.ErrorIs(t, err, test.want)
		})
	}
}

func TestAbsoluteWorkspacePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	write(t, root, "go.work", "go 1.27.1\nuse "+filepath.ToSlash(root)+"\n")
	_, err := project.Discover(root)
	require.ErrorIs(t, err, project.ErrUnsupportedLayout)
}

func TestWorkspaceParseCause(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	write(t, root, "go.work", "not a workspace\n")
	_, err := project.Discover(root)
	require.ErrorIs(t, err, project.ErrInvalidModule)

	var cause modfile.ErrorList
	require.ErrorAs(t, err, &cause)
	assert.NotEmpty(t, cause)
}

func TestIgnoredFixtureModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write(t, root, "go.mod", "module example.com/root\n")
	for _, directory := range []string{"testdata", "vendor", ".hidden", "_hidden"} {
		write(t, root, directory+"/go.mod", "invalid module file\n")
		write(t, root, directory+"/go.work", "invalid workspace file\n")
	}

	layout, err := project.Discover(root)
	require.NoError(t, err)
	assert.Equal(t, []project.Module{{Directory: root, Path: "example.com/root"}}, layout.Modules)
}

func write(t *testing.T, root, name, content string) {
	t.Helper()

	path := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
