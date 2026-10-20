# godelta

Go function call graphs scoped to a Git diff. Currently in development.

The analysis foundation loads a single module, inventories functions and methods, and resolves direct calls across packages. It gives closures and `init` declarations their own names, ignores formatting and comments when fingerprinting functions, and excludes tests and generated functions. Dynamic calls are counted without guessing their targets. Git comparisons and Mermaid output are not implemented yet; the executable remains a placeholder.

## Development

Install the pinned tools and Git hooks with `mise install`, then use:

```sh
mise run fix    # Apply formatting and available fixes without staging files
mise run check  # Verify formatting, lint, module tidiness, and race tests
mise run test   # Run race tests independently
```

The check and fix tasks build `bin/godelta-golangci-lint` with the custom `commentlen`, `goblanklines`, `goexpand`, and `goparamorder` linters.
