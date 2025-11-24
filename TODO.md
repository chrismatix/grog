# TODOs

- [ ] Parallelize directory writing and loading by generating write/load actions and then submitting them to a pool
- [ ] Introduce a max concurrency, pooling, and fail-safe rate limiting for the cas backend
- [ ] Improve shell completions to accept partial packages
- [ ] Add golangci-lint
- [ ] Add Azure blob storage option
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
  - Make this an option that can also error
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Tests: Use the new synctest package to better test the execution semantic

## Fixes

## Performance

- [ ] Add s3 multipart uploads
