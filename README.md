# godelta

See how Go functions change in a local diff or GitHub pull request. Godelta builds a call graph scoped to the changes, with enough surrounding code to see their connections.

## Install

Install with Homebrew:

```sh
brew install leonhfr/tap/godelta
```

Or download a Linux or macOS binary (amd64 or arm64) from [Releases](https://github.com/leonhfr/godelta/releases). Go and Git must be installed to analyze projects. PR mode also needs an authenticated [GitHub CLI](https://cli.github.com/).

## Local changes

Run from a Go repository or one of its subdirectories:

```sh
godelta                           # HEAD → working tree, including untracked files
godelta --staged                  # HEAD → staged changes only
godelta --base main               # Merge base with main → working tree
godelta --base main --head HEAD   # Merge base with main → committed HEAD
```

`--staged` cannot be combined with `--base` or `--head`. `--head` requires `--base`.

## Pull requests

```sh
gh auth login
godelta pr 123                          # Infer the repository from the checkout
godelta pr 123 -R owner/project         # Run from anywhere
godelta pr 123 -R github.example/owner/project
```

PR mode compares the merge base with the PR head, including fork PRs. Local changes are excluded. Both modes use temporary snapshots and leave your working tree, index, branches, and refs untouched.

## Output

A terminal gets a Unicode diagram rendered with [mermaid-ascii](https://github.com/AlexanderGrooff/mermaid-ascii). A pipe or file gets raw Mermaid:

```sh
godelta > graph.mmd
godelta pr 123 --format mermaid > pr.mmd
godelta --format mermaid                 # Mermaid plaintext in a terminal
godelta --format terminal | less -RS     # Browse a wide terminal diagram
```

Every added, removed, or modified function appears with its direct callers and callees from either revision. Context stops at one hop and is limited to 50 unchanged functions per connected group; changed functions are always kept. Diagnostics and omission counts go to stderr.

| Function | Color |
| --- | --- |
| Added | Green |
| Removed | Red |
| Modified | Grey |
| Unchanged context | Pale blue |

Arrows point from caller to callee and are labelled added, removed, or retained. Removed arrows are dotted in the terminal and dashed red in Mermaid. Terminal colors respect `NO_COLOR` and `TERM=dumb`. Large diagrams may need horizontal scrolling.

## Scope

- Functions and methods are compared by signature and body, ignoring formatting and comments. Global variables, constants, and struct changes do not seed the graph.
- Renames and package moves appear as removal plus addition. Anonymous functions use `Parent.anonymous.1`; init functions use `file.go.init.1`. Numbering follows source order and may shift after insertions.
- Direct calls, concrete methods, and generic calls are resolved. Interface dispatch and function-value calls are counted as unresolved, without guessed targets.
- Tests, generated functions, vendor code, and external dependency nodes are excluded.
- One module at the repository root is supported, with any number of packages. Workspaces, local replacements, nested modules outside fixture/vendor/hidden directories, symlinks, submodules, and sparse checkouts are unsupported.
- Both revisions must type-check under the active Go build configuration. Select build tags with `GOFLAGS='-tags=integration'`. Package loading may download dependencies; Godelta does not run project tests, binaries, or generators.

No function changes is a successful result. Use `godelta --help`, `godelta pr --help`, or `godelta --version` for command information.

## Development

```sh
mise install
mise exec -- go build -o godelta .
mise run fix          # Format and apply available fixes
mise run check        # Lint, configuration checks, and race tests
mise run goreleaser   # Build snapshot release archives without publishing
```
