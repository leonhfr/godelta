package analysis

import (
	"errors"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/leonhfr/godelta/internal/project"
)

var (
	// ErrInvalidModule indicates missing or invalid module/workspace configuration.
	ErrInvalidModule = project.ErrInvalidModule
	// ErrLoadPackages indicates that Go packages could not be loaded or checked.
	ErrLoadPackages = errors.New("load Go packages")
)

// PackageErrors retains the individual diagnostics reported by Go tooling.
// It matches ErrLoadPackages and unwraps to the original package errors.
type PackageErrors struct {
	Diagnostics []packages.Error
}

// Error formats the package diagnostics for display.
func (err *PackageErrors) Error() string {
	messages := make([]string, 0, len(err.Diagnostics))
	for _, diagnostic := range err.Diagnostics {
		messages = append(messages, diagnostic.Error())
	}

	return ErrLoadPackages.Error() + ":\n" + strings.Join(messages, "\n")
}

// Unwrap preserves the category and each original diagnostic for error matching.
func (err *PackageErrors) Unwrap() []error {
	causes := make([]error, 0, len(err.Diagnostics)+1)
	causes = append(causes, ErrLoadPackages)
	for _, diagnostic := range err.Diagnostics {
		causes = append(causes, diagnostic)
	}

	return causes
}
