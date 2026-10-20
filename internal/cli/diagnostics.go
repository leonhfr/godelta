package cli

import (
	"strconv"
	"strings"

	"github.com/leonhfr/godelta/internal/app"
	"github.com/leonhfr/godelta/internal/delta"
	"github.com/leonhfr/godelta/internal/graph"
)

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
