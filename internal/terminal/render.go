// Package terminal renders Mermaid review graphs with mermaid-ascii.
package terminal

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	asciirender "github.com/AlexanderGrooff/mermaid-ascii/pkg/render"

	"github.com/leonhfr/godelta/internal/graph"
	"github.com/leonhfr/godelta/internal/mermaid"
)

// Options controls optional ANSI status colors. Graph labels always include status words.
type Options struct{ Color bool }

var (
	entity = regexp.MustCompile(`#([0-9]+);`)
	labels = regexp.MustCompile(`\["([^"]*)"\]:::(added|removed|modified|unchanged)`)
)

// Render converts the exported Mermaid flowchart into a Unicode box diagram.
func Render(writer io.Writer, review graph.Graph, options Options) error {
	var source strings.Builder
	if err := mermaid.Render(&source, review); err != nil {
		return fmt.Errorf("prepare terminal graph: %w", err)
	}

	input := terminalSource(source.String())
	output, err := asciirender.RenderDiagram(input, diagram.DefaultConfig())
	if err != nil {
		return fmt.Errorf("render terminal graph: %w", err)
	}

	if options.Color {
		output = colorLabels(output, input)
	}

	if _, err := io.WriteString(writer, strings.TrimRight(output, "\n")+"\n"); err != nil {
		return fmt.Errorf("write terminal graph: %w", err)
	}

	return nil
}

// terminalSource adapts entities, label syntax, and styling to the upstream parser.
func terminalSource(input string) string {
	input = entity.ReplaceAllStringFunc(input, terminalEntity)
	// The upstream edge parser also recognizes open/circle/cross arrows inside
	// quoted text. Escape their leading hyphen in labels so paths remain labels.
	input = labels.ReplaceAllStringFunc(input, func(label string) string {
		return strings.NewReplacer("---", `\x2d--`, "--o", `\x2d-o`, "--x", `\x2d-x`).Replace(label)
	})
	input = strings.ReplaceAll(input, ",color:#111", "")
	input = strings.ReplaceAll(input, " -. removed .-> ", " -.->|removed| ")
	var lines []string
	for line := range strings.SplitSeq(input, "\n") {
		// Unsupported linkStyle declarations would otherwise become spurious nodes.
		if !strings.HasPrefix(strings.TrimSpace(line), "linkStyle ") {
			lines = append(lines, line)
		}
	}

	input = strings.Join(lines, "\n")
	return input
}

// colorLabels applies terminal colors without changing upstream global settings.
func colorLabels(output, input string) string {
	matches := labels.FindAllStringSubmatch(input, -1)
	// Longer labels take precedence when one function name is a suffix of another.
	slices.SortStableFunc(matches, func(a, b []string) int { return len(b[1]) - len(a[1]) })
	var replacements []string
	for _, match := range matches {
		replacements = append(replacements, match[1], colorLabel(match[1], match[2]))
	}

	return strings.NewReplacer(replacements...).Replace(output)
}

func terminalEntity(encoded string) string {
	value, _ := strconv.ParseInt(encoded[1:len(encoded)-1], 10, 32)
	character := rune(value)
	if unicode.IsGraphic(character) && !strings.ContainsRune("\"\\<>", character) {
		return string(character)
	}

	// Avoid \\n and <br> spellings that mermaid-ascii interprets as label breaks.
	if value < 256 {
		return fmt.Sprintf(`\x%02x`, value)
	}

	return fmt.Sprintf(`\u%04x`, value)
}

func colorLabel(text, status string) string {
	code := ""
	switch status {
	case "added":
		code = "32"

	case "removed":
		code = "31"

	case "modified":
		code = "90"

	case "unchanged":
		code = "94"
	}

	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
