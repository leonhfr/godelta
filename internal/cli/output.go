package cli

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"

	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/mermaid"
	"github.com/leonhfr/godelta/internal/terminal"
)

type format string

const (
	formatAuto     format = "auto"
	formatMermaid  format = "mermaid"
	formatTerminal format = "terminal"
)

func (value *format) String() string { return string(*value) }

func (value *format) Set(text string) error {
	switch format(text) {
	case formatAuto, formatMermaid, formatTerminal:
		*value = format(text)
		return nil
	}

	return fmt.Errorf("%w: unknown output format %q (choose auto, mermaid, or terminal)", ErrUsage, text)
}

func render(writer io.Writer, review graph.Graph, requested format) error {
	interactive := isTerminal(writer)
	if requested == formatMermaid || (requested == formatAuto && !interactive) {
		return mermaid.Render(writer, review)
	}

	color := interactive && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return terminal.Render(writer, review, terminal.Options{Color: color})
}

func isTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}

	return term.IsTerminal(int(file.Fd()))
}
