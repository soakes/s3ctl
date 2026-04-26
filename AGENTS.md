# Repository Guidelines

@/Users/soakes/.codex/RTK.md

## Project Structure & Ownership

- Keep the executable entrypoint in `cmd/s3ctl/`.
- Keep application logic under `internal/`; avoid adding new top-level Go
  packages at the repository root.
- Keep first-party automation, packaging, and release assets at the root,
  under `.github/`, `scripts/`, `examples/`, and `website/`.
- Treat `website/` as a first-party release surface, not a throwaway demo.

## Build, Lint, and Test Commands

- Install the pinned lint tool once per machine:
  ```bash
  make lint-install
  ```
- Format Go code:
  ```bash
  make fmt
  ```
- Check baseline formatting:
  ```bash
  make fmt-check
  ```
- Run the pinned lint suite:
  ```bash
  make lint
  ```
- Apply available lint fixes:
  ```bash
  make lint-fix
  ```
- Run static validation:
  ```bash
  make vet
  ```
- Run tests:
  ```bash
  make test
  ```
- Build the local binary:
  ```bash
  make build
  ```
- Lint shell scripts:
  ```bash
  shellcheck scripts/*.sh
  ```

## Go Style & Quality Rules

- Always use `gofmt` at minimum; prefer the pinned `golangci-lint` toolchain so
  `gofumpt`, `goimports`, `staticcheck`, `errcheck`, and `revive` are applied
  consistently.
- Favor clear, operator-facing errors that preserve the failing path,
  operation, or target resource.
- Keep exported APIs and package-level behavior documented when lint rules
  require it.
- Prefer small helpers and focused structs over large generic utility layers.
- Keep CLI help, examples, and environment variable names polished and ready to
  copy into production workflows.

## Workflow & Security Rules

- Treat workflow changes as security-sensitive. Keep GitHub Actions permissions
  least-privilege and scoped to the jobs that need them.
- Pull requests should validate code and packaging without gaining access to
  signing or publish credentials.
- If workflow behavior changes, update the README and any operator-facing docs
  in the same change when user-visible behavior changes.
- Treat destructive storage operations as security-sensitive. Keep object
  removal guarded by explicit confirmation, allow unforced bucket deletes only
  after proving the bucket is empty, preserve dry-run behavior, and verify
  provider ownership assumptions before deleting credentials or users.

## Testing Expectations

- Minimum validation for Go code changes:
  ```bash
  make fmt-check
  make lint
  make vet
  make test
  make build
  ```
- If you change shell scripts, also run:
  ```bash
  bash -n scripts/*.sh
  shellcheck scripts/*.sh
  ```
- If you change website or release automation behavior, run the matching local
  validation that exercises those paths before considering the change complete.

## Commit & PR Rules

- Do not create commits unless the user explicitly asks for a commit in the
  current turn.
- Prefer conventional commit subjects such as `feat:`, `fix:`, `docs:`, `ci:`,
  `deps:`, `chore:`, or `test:`.
- Keep changes focused by concern when practical: runtime logic, automation,
  docs, packaging, and website work should not be bundled casually.
- PR descriptions should explain the operational reason for the change, the
  validation performed, and any release or upgrade impact.
