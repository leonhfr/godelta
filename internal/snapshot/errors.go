package snapshot

import "errors"

var (
	// ErrInvalidOptions indicates an incompatible comparison selection.
	ErrInvalidOptions = errors.New("invalid comparison options")
	// ErrNotRepository indicates that a Git checkout could not be found.
	ErrNotRepository = errors.New("not a Git checkout")
	// ErrInvalidRevision indicates an unresolved revision or missing merge base.
	ErrInvalidRevision = errors.New("invalid comparison revision")
	// ErrMergeConflict indicates unresolved index entries.
	ErrMergeConflict = errors.New("unresolved merge conflicts")
	// ErrUnsupportedLayout indicates inputs unsupported by isolated snapshots.
	ErrUnsupportedLayout = errors.New("unsupported repository layout")
	// ErrInvalidModule indicates a missing or invalid root module declaration.
	ErrInvalidModule = errors.New("invalid Go module")
	// ErrInvalidPath indicates a path that cannot be included in a snapshot.
	ErrInvalidPath = errors.New("invalid repository path")
	// ErrInvalidGitOutput indicates malformed Git object or tree data.
	ErrInvalidGitOutput = errors.New("invalid Git output")
)
