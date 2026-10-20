// Package cli presents local Go diff review as Mermaid on stdout.
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
	"github.com/leonhfr/godelta/internal/mermaid"
	"github.com/leonhfr/godelta/internal/snapshot"
)

// ErrUsage identifies invalid command arguments.
var ErrUsage = errors.New("invalid command arguments")

// Run reviews a checkout and writes raw Mermaid with diagnostics on stderr.
func Run(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error {
	options, err := parse(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}

	if err != nil {
		return err
	}

	result, err := app.Review(ctx, dir, options)
	if err != nil {
		return err
	}

	if _, err = io.WriteString(stderr, diagnostics(result)); err != nil {
		return fmt.Errorf("write review diagnostics: %w", err)
	}

	return mermaid.Render(stdout, result.Graph)
}

func parse(args []string, stderr io.Writer) (snapshot.Options, error) {
	flags := flag.NewFlagSet("godelta", flag.ContinueOnError)
	var usage bytes.Buffer
	flags.SetOutput(&usage)
	var options snapshot.Options
	flags.BoolVar(&options.Staged, "staged", false, "Compare HEAD with staged content only")
	flags.StringVar(&options.Base, "base", "", "Compare from the merge base of this revision and the selected head")
	flags.StringVar(&options.Head, "head", "", "Compare a committed head instead of the working tree (requires --base)")
	parseErr := flags.Parse(args)
	if usage.Len() != 0 {
		if _, err := io.WriteString(stderr, usage.String()); err != nil {
			return snapshot.Options{}, fmt.Errorf("write command usage: %w", err)
		}
	}

	if err := parseErr; err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return snapshot.Options{}, err
		}

		return snapshot.Options{}, fmt.Errorf("%w: %w", ErrUsage, err)
	}

	if flags.NArg() != 0 {
		return snapshot.Options{}, fmt.Errorf("%w: unexpected arguments %q", ErrUsage, flags.Args())
	}

	return options, nil
}

func diagnostics(result app.Result) string {
	var text strings.Builder
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
