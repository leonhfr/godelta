# godelta

Go function call graphs scoped to a Git diff. Currently in development.

The analysis foundation loads a single module, inventories functions and methods, and resolves direct calls across packages. It gives closures and `init` declarations their own names, ignores formatting and comments when fingerprinting functions, and excludes tests and generated functions. Dynamic calls are counted without guessing their targets. The snapshot layer prepares isolated working-tree, staged-only, and merge-base comparisons without changing the checkout, index, or refs. Working-tree mode includes nonignored untracked files; staged-only mode reads the index and excludes intent-to-add files. Both sides retain supporting files needed for analysis, such as embedded assets.

Snapshots currently require a module at the repository root. Workspaces, nested modules, local module replacements, symlinks, submodules, and sparse working checkouts are unsupported and produce explicit errors.

The delta layer classifies functions as added, removed, modified, or unchanged and calls as added, removed, or retained. It preserves both versions of each function for source locations and diagnostics, with deterministic ordering and no mutation of the input snapshots. Renames and package moves appear as removal plus addition; moves between files within a package preserve function identity.

Scoped graph selection and Mermaid output are not implemented yet; the executable remains a placeholder.

## Development

Install the pinned tools and Git hooks with `mise install`, then use:

```sh
mise run fix    # Apply formatting and available fixes without staging files
mise run check  # Verify formatting, lint, module tidiness, and race tests
mise run test   # Run race tests independently
```

The check and fix tasks build `bin/godelta-golangci-lint` with the custom `commentlen`, `goblanklines`, `goexpand`, and `goparamorder` linters.
