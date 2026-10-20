// Package cli presents Go diff review as Mermaid or a terminal graph.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/leonhfr/godelta/internal/app"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/snapshot"
)

// ErrUsage identifies invalid command arguments.
var ErrUsage = errors.New("invalid command arguments")

// Run reviews a checkout or PR and writes the selected format with diagnostics on stderr.
func Run(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error {
	request, err := parseRequest(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}

	if err != nil {
		return err
	}

	result, err := review(ctx, dir, request)
	if err != nil {
		return err
	}

	if _, err = io.WriteString(stderr, diagnostics(result)); err != nil {
		return fmt.Errorf("write review diagnostics: %w", err)
	}

	return render(stdout, result.Graph, request.format)
}

type request struct {
	local      snapshot.Options
	number     int
	repository string
	format     format
}

func review(ctx context.Context, dir string, request request) (app.Result, error) {
	if request.number != 0 {
		return app.ReviewPR(ctx, dir, request.repository, request.number)
	}

	return app.Review(ctx, dir, request.local)
}

func parseRequest(args []string, stderr io.Writer) (request, error) {
	if len(args) > 0 && args[0] == "pr" {
		return parsePR(args[1:], stderr)
	}

	return parse(args, stderr)
}

func parsePR(args []string, stderr io.Writer) (request, error) {
	flags := flag.NewFlagSet("godelta pr", flag.ContinueOnError)
	var usage bytes.Buffer
	flags.SetOutput(&usage)
	options := request{format: formatAuto}
	flags.Var(&options.format, "format", "Output format: auto, mermaid, terminal")
	flags.StringVar(&options.repository, "repo", "", "Repository as [HOST/]OWNER/REPO (defaults to this checkout)")
	flags.StringVar(&options.repository, "R", "", "Alias for --repo")
	// The documented number-first form must also accept flags after the number.
	numberText := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		numberText = args[0]
		args = args[1:]
	}

	parseErr := flags.Parse(args)
	if usage.Len() != 0 {
		if _, err := io.WriteString(stderr, usage.String()); err != nil {
			return request{}, fmt.Errorf("write command usage: %w", err)
		}
	}

	if errors.Is(parseErr, flag.ErrHelp) {
		return request{}, parseErr
	}

	if parseErr != nil {
		return request{}, fmt.Errorf("%w: %w", ErrUsage, parseErr)
	}

	if numberText == "" && flags.NArg() == 1 {
		numberText = flags.Arg(0)
	} else if flags.NArg() != 0 {
		return request{}, ErrUsage
	}

	number, err := strconv.Atoi(numberText)
	if err != nil || number <= 0 || strconv.Itoa(number) != numberText {
		return request{}, fmt.Errorf("%w: pr requires a positive PR number", ErrUsage)
	}

	options.number = number
	return options, nil
}

func parse(args []string, stderr io.Writer) (request, error) {
	flags := flag.NewFlagSet("godelta", flag.ContinueOnError)
	var usage bytes.Buffer
	flags.SetOutput(&usage)
	flags.Usage = func() {
		fmt.Fprintln(&usage, "Usage: godelta [flags]\n       godelta pr NUMBER [-R [HOST/]OWNER/REPO]")
		flags.PrintDefaults()
	}

	options := request{format: formatAuto}
	flags.Var(&options.format, "format", "Output format: auto, mermaid, terminal")
	flags.BoolVar(&options.local.Staged, "staged", false, "Compare HEAD with staged content only")
	flags.StringVar(&options.local.Base, "base", "", "Compare from the merge base of this revision and the selected head")
	flags.StringVar(&options.local.Head, "head", "", "Compare a committed head instead of the working tree (requires --base)")
	parseErr := flags.Parse(args)
	if usage.Len() != 0 {
		if _, err := io.WriteString(stderr, usage.String()); err != nil {
			return request{}, fmt.Errorf("write command usage: %w", err)
		}
	}

	if err := parseErr; err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return request{}, err
		}

		return request{}, fmt.Errorf("%w: %w", ErrUsage, err)
	}

	if flags.NArg() != 0 {
		return request{}, fmt.Errorf("%w: unexpected arguments %q", ErrUsage, flags.Args())
	}

	return options, nil
}

func diagnostics(result app.Result) string {
	var text strings.Builder
	if result.PullRequestURL != "" {
		text.WriteString("pull request: " + result.PullRequestURL + "\n")
	}

	text.WriteString("comparison: " + string(result.Mode) + " (base " + result.BaseCommit + "; head " + result.HeadCommit + ")\n")
	text.WriteString("build: " + result.Build.OS + "/" + result.Build.Arch + "; cgo=" + result.Build.CGOEnabled +
		"; GOFLAGS=" + strconv.Quote(result.Build.Flags) + "\n")
	if len(result.Graph.Components) == 0 {
		text.WriteString("No function changes.\n")
	}

	for index, component := range result.Graph.Components {
		componentDiagnostics(&text, component, index+1)
	}

	return text.String()
}

func componentDiagnostics(text *strings.Builder, component graph.Component, index int) {
	changed := 0
	for _, fn := range component.Functions {
		if fn.Status != delta.Unchanged {
			changed++
		}

		before, after := 0, 0
		if fn.Before != nil {
			before = fn.Before.DynamicCalls
		}

		if fn.After != nil {
			after = fn.After.DynamicCalls
		}

		if before != 0 || after != 0 {
			text.WriteString("unresolved calls: " + string(fn.ID) + " (before " + strconv.Itoa(before) + "; after " + strconv.Itoa(after) + ")\n")
		}
	}

	prefix := "component " + strconv.Itoa(index) + ": "
	if component.OmittedContext != 0 {
		text.WriteString(prefix + "omitted " + strconv.Itoa(component.OmittedContext) + " unchanged context functions (limit " +
			strconv.Itoa(graph.ContextLimit) + ")\n")
	}

	if changed > graph.ContextLimit {
		text.WriteString(prefix + strconv.Itoa(changed) + " changed functions; rendering all (large diagram)\n")
	}
}
