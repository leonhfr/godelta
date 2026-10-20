// Package cli defines the Godelta command and its review actions.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	urfave "github.com/urfave/cli/v3"

	"github.com/leonhfr/godelta/internal/app"
	"github.com/leonhfr/godelta/internal/snapshot"
)

// ErrUsage identifies invalid command arguments.
var ErrUsage = errors.New("invalid command arguments")

// Option configures a Godelta command.
type Option func(*settings)

type settings struct {
	version   string
	directory string
}

// WithVersion sets the build version. An empty value defaults to dev.
func WithVersion(version string) Option {
	return func(options *settings) { options.version = version }
}

// WithDirectory sets the directory used for Git and Go commands.
// An empty value uses the current working directory.
func WithDirectory(directory string) Option {
	return func(options *settings) { options.directory = directory }
}

// New returns a command ready to Run with a context and process arguments.
// Output streams can be configured through the command's Writer and ErrWriter.
func New(opts ...Option) *urfave.Command {
	var options settings
	for _, option := range opts {
		option(&options)
	}

	if options.version == "" {
		options.version = "dev"
	}

	if options.directory == "" {
		options.directory = "."
	}

	return &urfave.Command{
		Name:      "godelta",
		Usage:     "Show a call graph of Go function changes",
		Version:   options.version,
		ArgsUsage: " ",
		// Main owns process exit, including errors from Git and Go subprocesses.
		ExitErrHandler: func(context.Context, *urfave.Command, error) {},
		OnUsageError:   usageError,
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "format", Value: string(formatAuto), Usage: "Output format: auto, mermaid, terminal", Validator: validateFormat},
			&urfave.BoolFlag{Name: "staged", Local: true, Usage: "Compare HEAD with staged content only"},
			&urfave.StringFlag{Name: "base", Local: true, Usage: "Compare from the merge base of this revision and the selected head"},
			&urfave.StringFlag{Name: "head", Local: true, Usage: "Compare a committed head instead of the working tree (requires --base)"},
		},
		Action: func(ctx context.Context, command *urfave.Command) error {
			if command.Args().Len() != 0 {
				return fmt.Errorf("%w: unexpected arguments %q", ErrUsage, command.Args().Slice())
			}

			result, err := app.Review(ctx, options.directory, snapshot.Options{
				Staged: command.Bool("staged"),
				Base:   command.String("base"),
				Head:   command.String("head"),
			})
			if err != nil {
				return err
			}

			return writeReview(command, result)
		},
		Commands: []*urfave.Command{prCommand(options.directory)},
	}
}

func prCommand(directory string) *urfave.Command {
	return &urfave.Command{
		Name:         "pr",
		Usage:        "Review a GitHub pull request",
		ArgsUsage:    "NUMBER",
		OnUsageError: usageError,
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "repo", Aliases: []string{"R"}, Usage: "Repository as [HOST/]OWNER/REPO (defaults to this checkout)"},
		},
		Action: func(ctx context.Context, command *urfave.Command) error {
			for _, name := range []string{"staged", "base", "head"} {
				if command.Root().IsSet(name) {
					return fmt.Errorf("%w: --%s applies only to local comparisons", ErrUsage, name)
				}
			}

			text := command.Args().First()
			number, err := strconv.Atoi(text)
			if command.Args().Len() != 1 || err != nil || number <= 0 || strconv.Itoa(number) != text {
				return fmt.Errorf("%w: pr requires a positive PR number", ErrUsage)
			}

			result, err := app.ReviewPR(ctx, directory, command.String("repo"), number)
			if err != nil {
				return err
			}

			return writeReview(command, result)
		},
	}
}

func writeReview(command *urfave.Command, result app.Result) error {
	if _, err := io.WriteString(command.Root().ErrWriter, diagnostics(result)); err != nil {
		return fmt.Errorf("write review diagnostics: %w", err)
	}

	return render(command.Root().Writer, result.Graph, format(command.String("format")))
}

func usageError(_ context.Context, _ *urfave.Command, err error, _ bool) error {
	return fmt.Errorf("%w: %w", ErrUsage, err)
}
