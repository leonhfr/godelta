# godelta

Generate a call graph of Go function changes in a local Git checkout or a GitHub pull request.

## Usage

Build with the pinned Go toolchain:

```sh
mise install
mise exec -- go build -o godelta .
```

Run the binary from the Go repository you want to review, or any subdirectory of it:

```sh
godelta > graph.mmd                       # HEAD versus the working tree
godelta --staged > graph.mmd              # HEAD versus staged content only
godelta --base main > graph.mmd           # Merge base with main versus the working tree
godelta --base main --head HEAD > graph.mmd # Merge base with main versus committed HEAD
godelta --help
```

Use `./godelta` when reviewing this checkout, or put the built binary on your PATH for other repositories. Working-tree mode includes staged edits, unstaged edits, and nonignored untracked files. Staged mode excludes unstaged edits and files marked only with `git add -N`. `--head` requires `--base`; `--staged` cannot be combined with either.

Output defaults to `--format auto`: a Unicode box diagram when stdout is a terminal, or one raw Mermaid flowchart when piped or redirected. Both local and PR commands accept an explicit override:

```sh
godelta --format mermaid                  # Mermaid plaintext, including in a terminal
godelta --format terminal | less -R       # Browse a terminal graph through a pager
godelta pr 123 --format terminal          # Terminal view of a PR
godelta pr 123 --format mermaid > graph.mmd
```

Stderr reports resolved revisions, the active Go platform and flags, omitted context, and unresolved call counts. No function changes produces an explanation and a successful exit. Invalid inputs, analysis failures, and output failures produce a nonzero exit.

## GitHub pull requests

Install and authenticate [GitHub CLI](https://cli.github.com/manual/gh_auth_login), then select a PR:

```sh
godelta pr 123 > graph.mmd                       # Repository inferred from the checkout
godelta pr 123 --repo owner/project > graph.mmd  # Works outside a checkout too
godelta pr 123 -R github.example/owner/project > graph.mmd
godelta pr --help
```

PR mode compares the merge base with the PR head, including fork PRs. Metadata comes from `gh`; Git fetches the required revisions into a temporary bare repository using `gh` credentials. Local branches, refs, index, and working files are untouched. Local edits do not enter a PR comparison. If the head moves between metadata lookup and fetch, rerun the command. PR URLs and resolved commits appear on stderr.

## Reading the graph

Every added, removed, or modified function appears, along with its direct project-local callers and callees from either revision. Context does not expand recursively. Disconnected groups share one flowchart, with up to 50 unchanged context functions per group. Changed functions never count against that limit and are never omitted. Context is selected in canonical name order, with omission counts on stderr.

| Function status | Color |
| --- | --- |
| Added | Green |
| Removed | Red |
| Modified | Grey |
| Unchanged context | Pale blue |

The terminal view uses [mermaid-ascii](https://github.com/AlexanderGrooff/mermaid-ascii) to render the same flowchart as Unicode boxes and arrows, including change groups, shared callees, cycles, and the legend. Function and call statuses remain visible as text. Labels are kept in full, so large graphs may require horizontal scrolling. Source names containing terminal controls or syntax interpreted by the renderer use escaped spellings.

Terminal function colors are enabled only when stdout is a terminal, `TERM` is not `dumb`, and `NO_COLOR` is unset or empty. Piped terminal output is plain Unicode text. Removed calls use dotted arrows; terminal arrows use plain text rather than Mermaid stroke colors. The renderer is linked into the binary and needs no external executable.

In Mermaid output, arrows point from caller to callee. Added calls are green, removed calls are dashed red, and retained calls are grey. Nodes and arrows include status words, and the graph includes a legend.

## Analysis scope

Functions are matched by package, receiver type, and name, and compared by their signatures and bodies. Formatting and comments are ignored. File moves within a package preserve identity; renames and package moves appear as removal plus addition. Global variables, constants, and struct declarations do not seed changes.

Anonymous functions have separate nodes named after their parent, such as `Save.anonymous.1`. Init declarations use file names, such as `storage.go.init.1`. Counters follow source order and may shift after insertions. Calls inside a closure belong to the closure; declaring it does not imply calling it.

Direct calls, concrete methods, generic calls, and immediately invoked function literals are resolved. Interface dispatch and calls through function values are counted as unresolved; targets are not guessed. Tests, generated functions, vendor functions, and external dependency nodes are excluded. Analysis covers only the active build configuration; use Go environment settings such as `GOFLAGS='-tags=integration'` to select build tags.

The repository must contain one module at its root. Workspaces, actual nested modules, local module replacements, symlinks, submodules, and sparse working checkouts are currently unsupported. Nested testdata, vendor, and hidden module fixtures are outside layout checks. A committed HEAD and analyzable code on both sides are required.

Snapshots are copied into temporary directories without checking out revisions, writing the index, or moving refs, and are removed on success or failure. Supporting files, including embedded assets, are copied too. The tool never runs project binaries, tests, or generators. Go tooling must be installed; package loading may resolve dependencies using Go's module cache and network settings. Module files are loaded read-only.

The GitHub CLI extension entry point is planned. Local review does not require `gh`.

## Development

Use the pinned tools and Git hooks from `mise install`:

```sh
mise run fix    # Apply formatting and available fixes without staging files
mise run check  # Verify formatting, lint, module tidiness, and race tests
mise run test   # Run race tests independently
```

The check and fix tasks build `bin/godelta-golangci-lint` with the custom `commentlen`, `goblanklines`, `goexpand`, and `goparamorder` linters. Tests use Testify; error assertions use error categories and structured diagnostics rather than message matching.
