// Package mermaid renders review graphs as raw Mermaid flowcharts.
package mermaid

import (
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/leonhfr/godelta/internal/analysis"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
)

// Render writes one flowchart with stable node identifiers and status styling.
func Render(writer io.Writer, review graph.Graph) error {
	var diagram strings.Builder
	diagram.WriteString("flowchart LR\n")
	if len(review.Components) == 0 {
		diagram.WriteString("    no_changes[\"No function changes\"]\n")
	} else {
		renderGraph(&diagram, review)
	}

	if _, err := io.WriteString(writer, diagram.String()); err != nil {
		return fmt.Errorf("write Mermaid diagram: %w", err)
	}

	return nil
}

func renderGraph(diagram *strings.Builder, review graph.Graph) {
	var styles strings.Builder
	edgeIndex := 0
	for index, component := range review.Components {
		diagram.WriteString("    subgraph component_" + strconv.Itoa(index) + "[\"Change group " + strconv.Itoa(index+1) + "\"]\n")
		for _, fn := range component.Functions {
			source := fn.After
			if source == nil {
				source = fn.Before
			}

			name := source.Name
			if directory := path.Dir(source.File); directory != "." {
				name = directory + "." + name
			}

			label := escape(name + " · " + string(fn.Status))
			diagram.WriteString("        " + nodeID(fn.ID) + "[\"" + label + "\"]:::" + string(fn.Status) + "\n")
		}

		for _, call := range component.Calls {
			renderCall(diagram, &styles, nodeID(call.Call.Caller), nodeID(call.Call.Callee), call.Status, edgeIndex)
			edgeIndex++
		}

		diagram.WriteString("    end\n")
	}

	diagram.WriteString("    subgraph legend[\"Legend · arrows point from caller to callee\"]\n" +
		"        legend_added[\"Added function\"]:::added\n" +
		"        legend_removed[\"Removed function\"]:::removed\n" +
		"        legend_modified[\"Modified function\"]:::modified\n" +
		"        legend_context[\"Unchanged context\"]:::unchanged\n")
	renderCall(diagram, &styles, "legend_added", "legend_modified", delta.CallAdded, edgeIndex)
	renderCall(diagram, &styles, "legend_modified", "legend_removed", delta.CallRemoved, edgeIndex+1)
	renderCall(diagram, &styles, "legend_context", "legend_modified", delta.CallRetained, edgeIndex+2)
	diagram.WriteString("    end\n" +
		"    classDef added fill:#dcfce7,stroke:#15803d,color:#111;\n" +
		"    classDef removed fill:#fee2e2,stroke:#b91c1c,color:#111;\n" +
		"    classDef modified fill:#e5e7eb,stroke:#6b7280,color:#111;\n" +
		"    classDef unchanged fill:#dbeafe,stroke:#2563eb,color:#111;\n")
	diagram.WriteString(styles.String())
}

func renderCall(diagram, styles *strings.Builder, caller, callee string, status delta.CallStatus, index int) {
	arrow := ""
	style := ""
	switch status {
	case delta.CallAdded:
		arrow = " -->|added| "
		style = "stroke:#15803d,stroke-width:2px"

	case delta.CallRemoved:
		arrow = " -. removed .-> "
		style = "stroke:#b91c1c,stroke-width:2px"

	case delta.CallRetained:
		arrow = " -->|retained| "
		// A final hex value is misread as an entity by Mermaid's preprocessing.
		style = "stroke:#6b7280,stroke-width:1px"
	}

	diagram.WriteString("        " + caller + arrow + callee + "\n")
	styles.WriteString("    linkStyle " + strconv.Itoa(index) + " " + style + ";\n")
}

func nodeID(id analysis.ID) string {
	return fmt.Sprintf("f_%x", sha256.Sum256([]byte(id)))
}

func escape(label string) string {
	var escaped strings.Builder
	for _, character := range label {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune(" ./():*_+-,", character) {
			escaped.WriteRune(character)
		} else {
			escaped.WriteString("#" + strconv.Itoa(int(character)) + ";")
		}
	}

	return escaped.String()
}
