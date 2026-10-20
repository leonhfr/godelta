# godelta

Go function call graphs scoped to a Git diff. Currently in development.

## Development

Install the pinned tools and Git hooks with `mise install`, then use:

```sh
mise run fix    # Apply formatting and available fixes without staging files
mise run check  # Verify formatting, lint, module tidiness, and race tests
mise run test   # Run race tests independently
```

The check and fix tasks build `bin/godelta-golangci-lint` with the custom `commentlen`, `goblanklines`, `goexpand`, and `goparamorder` linters.
