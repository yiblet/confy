# Contributing to Confy

Thanks for contributing. Confy is a small, dependency-light configuration
library, so changes should keep its API explicit, composable, and easy to
document.

## Prerequisites

- Go 1.27 or newer
- Git

## Development workflow

Fork the repository, create a focused branch, and make the change with tests.
Before opening a pull request, run:

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -w $(git ls-files '*.go')
git diff --check
```

Avoid adding a runtime dependency unless it is essential. Confy's public API
is intentionally reflection-free and its generated documentation, template,
and JSON Schema should stay consistent with every reader combinator.

## Pull requests

Describe the problem and the chosen behavior, include tests for a bug fix or
new behavior, and update the README or package documentation when a user
visible API changes. Keep unrelated reformatting out of the pull request.

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
