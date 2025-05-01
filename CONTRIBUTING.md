# Contributing

We love every form of contribution. By participating in this project, you
agree to abide to the [code of conduct](/code_of_conduct.md).

When opening a Pull Request, check that:

- Tests are passing
- Code is linted
- Description references the issue
- The branch name follows our convention
- Commits are squashed and follow our conventions

## Release Flow

- Tag and push the current main branch: `git tag v0.1.0 && git push origin v0.1.0`
  - This will trigger the release flow in `.github/release.yml`
- Manually review and then publish the draft release.
- Update the homebrew formula.
