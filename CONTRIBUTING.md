# Contributing to ROSA Trusted Actions Server

Thank you for your interest in contributing to the ROSA Trusted Actions Server. This document outlines the process and guidelines for contributing.

## Getting Started

1. Fork the repository
2. Clone your fork locally
3. Create a feature branch from `main`
4. Make your changes
5. Submit a pull request

See [DEVELOPMENT.md](DEVELOPMENT.md) for setting up your local development environment.

## Code of Conduct

This project follows the [Red Hat Community Code of Conduct](https://www.redhat.com/en/about/open-source/participation-guidelines). Please read and follow it in all interactions.

## Pull Request Process

### Before Submitting

1. **Run linting**: `make lint` — all code must pass `golangci-lint` with the project's `.golangci.yml` configuration.
2. **Run tests**: `make test` — all existing tests must pass.
3. **Add tests**: New code must include unit tests. Patch coverage must be >= 80% (enforced by Codecov).
4. **Format code**: `make fmt` — code must be formatted with `go fmt` and `goimports`.
5. **Pre-commit hooks**: Install and run pre-commit hooks (`pre-commit install && pre-commit run --all-files`).

### PR Requirements

- PRs must target the `main` branch.
- PRs must pass all CI checks (lint, test, build, OpenAPI validation).
- PRs must have a clear title and description explaining the change and its motivation.
- PRs modifying the OpenAPI spec must regenerate code with `make generate` and include the updated generated files.

### AI-Assisted Contributions

AI-assisted contributions are welcome and follow the same quality standards as human-authored code. If you use an AI coding assistant:

- Include `Co-Authored-By` trailers in commit messages to attribute AI contributions.
- AI-assisted PRs are automatically labeled for separate tracking and metrics.
- All AI-generated code must meet the same review, testing, and quality standards.

### Review Process

- All PRs require at least one approving review from a code owner.
- Reviewers should check for correctness, test coverage, security implications, and adherence to project conventions.
- Use `/lgtm` and `/approve` Prow commands if applicable.

## What to Contribute

- Bug fixes
- Test coverage improvements
- Documentation improvements
- New API features (discuss in an issue first)
- Security improvements
- CI/CD improvements

## Reporting Issues

Open an issue in the repository describing:
- What you expected to happen
- What actually happened
- Steps to reproduce
- Relevant logs or error messages

## Commit Messages

- Use clear, concise commit messages.
- Start with an imperative verb (e.g., "Add", "Fix", "Update", "Remove").
- Reference related issues where applicable (e.g., "Fixes #123").

## Questions?

If you have questions about contributing, open an issue or reach out to the maintainers listed in the [OWNERS](OWNERS) file.
