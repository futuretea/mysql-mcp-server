# Repository Instructions

## Scope

Keep the template generic. Prefer existing Go, Makefile, Docker, and GitHub
Actions conventions over project-specific tooling or release assumptions.

## Initialization

Run `./scripts/init-template.sh` before building a repository created from
this template. It replaces the documented placeholder values and installs the
build CI template. Pass `--with-release` only when the derived repository is
ready to configure npm and Docker publication.

## Conventions

- Keep code, workflow identifiers, and comments in English.
- Keep user-facing repository documentation in English and Chinese when both
  editions already exist.
- Do not add secrets, registry names, package scopes, or repository-specific
  release settings to reusable CI assets.
- Keep generated binaries, package names, and supported platforms in one
  explicit inventory per consuming project.

## Verification

Run the narrowest relevant checks before delivery. For Go and CI changes,
start with `make ci`; validate changed GitHub Actions with `actionlint` when
it is available. Do not commit or publish unless the user explicitly asks.
