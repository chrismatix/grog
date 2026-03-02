# Agent Instructions for the `grog` Repository

## Repository overview
- `internal/` holds almost all of the Go packages that power the CLI (loading, analysis, execution, caching, console UI, etc.).
- `integration/` contains end-to-end CLI tests that exercise compiled binaries against sample repositories.
- `docs/` is an [Astro](https://astro.build/) documentation site; `docs/gen_docs.go` regenerates the CLI reference pages from the Cobra command definitions.
- `examples/`, `pkl/`, and the shell scripts at the root are support assets that changes occasionally need to keep in sync.

## General workflow expectations
- Keep changes cohesive and favor small, focused patches that update code, tests, and docs together.
- When you add new inputs (flags, config knobs, etc.), search the repo for related documentation or fixtures and update them in the same change.
- Prefer existing helpers/utilities (for logging, hashing, path handling, etc.) instead of reimplementing similar logic in new places.

## Go code style and conventions
- Run `gofmt` (or an editor that uses it) on every modified `.go` file. Go files in this repo use standard Go formatting, with tabs for indentation and grouped imports.
- If you add or change imports, also run `goimports` (or `gofmt -w` if `goimports` is unavailable and the import groups are simple).
- Follow idiomatic Go error handling: wrap errors with `fmt.Errorf("...: %w", err)` when providing context, prefer `errors.Is` / `errors.As` for comparisons, and avoid swallowing errors silently.
- Functions that perform I/O, blocking work, or are part of the execution pipeline should accept a `context.Context` as their first parameter when practical. Thread the context through to downstream calls instead of creating background contexts.
- Tests should be table-driven when the same logic is exercised with multiple inputs, should use `t.Run` subtests, and any helper functions that call `t.Fatalf`/`t.Helper` should invoke `t.Helper()`.
- Keep new public (exported) identifiers documented with sentence-case comments.
- **Avoid abbreviations in variable names**: Spell out descriptive names in full. For example, use `loadContext` instead of `loadCtx`, `relativePath` instead of `relPath`, `key` and `value` instead of `k` and `v`. This applies even when shorter identifiers might be considered idiomatic in other Go projects.

## Go module and dependency management
- If you add or upgrade Go dependencies, run `go mod tidy` before committing so that `go.mod` and `go.sum` stay minimal.
- Generated code should be committed if the repository currently keeps the generated artefacts under version control. Check existing patterns in the touched package before adding new generators.

## Testing expectations
- For Go code changes run `go test ./...` (or at least the relevant packages). This is the baseline check that must pass before completion.
- If you modify integration test code, test scenarios, or behavior that affects CLI output, also run `go test ./integration/...` to ensure the CLI fixtures stay green. Use `make test update=<test_case>` if you need to refresh a single fixture.
- When you regenerate the CLI docs (`docs/gen_docs.go`), verify that the generated files build by running `npm install` (once) and `npm run build` inside `docs/` if you touched the Astro site or its assets.

## CLI command and documentation updates
- Whenever you add or change Cobra commands/flags under `internal/cmd`, run `cd docs && go run gen_docs.go` so that the reference docs in `docs/src/content/docs/reference/cli` stay in sync.
- Review the generated Markdown for accuracy and commit those updates alongside the code changes.
- Check `docs/src/content/docs/` for conceptual guides or tutorials that may also need to mention new functionality.

## Documentation site (`docs/`)
- Follow Astro/TypeScript conventions already present in the repo. Keep Markdown front matter intact (`title`, etc.) and prefer relative asset links under `src/assets`.
- Use `npm run build` to catch type or Astro config errors after editing docs or Astro components. If you introduce new npm dependencies, commit the updated `package-lock.json`.

## Integration test fixtures
- Fixtures under `integration/fixtures` are plain text snapshots. Avoid hand-editing them; instead, update them via the test harness (`make test update=<case>` or by re-running the relevant Go test) to keep formatting consistent.
- New test scenarios belong in `integration/test_scenarios/*.yaml`. Ensure scenario names are unique and describe what they cover.
- Each scenario case's `repo` points to a directory under `integration/test_repos`; those repositories provide the on-disk project that the CLI operates on during the test. Copy an existing repo (or build a new one) there before wiring up a scenario that references it, and refresh the fixtures after editing the repo contents.

## PKL configuration
- Changes to the PKL project in `pkl/` should keep the project packageable via `cd pkl && pkl project package`. Update `PklProject.deps.json` only when dependency versions change.

## Commit & PR etiquette
- Keep commits logically grouped and leave the worktree clean (`git status` shows no changes) before finishing.
- Summaries and PR descriptions should clearly call out user-visible changes and any new tests or docs that were added.
