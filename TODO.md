# TODOs


- [ ] Implement enable_cache
- [ ] Figure out a way to only load docker images when needed
  - https://bazel.build/reference/command-line-reference#common_options-flag--remote_download_all
  - This but instead only load outputs when their **direct** dependants changed
- [ ] Make sure that sigterm always kills embedded commands after a grace period
  - It didn't work when running something like `npm run dev` inside the command
- [ ] Add shell completions for commands that run targets
- [ ] Add golangci-lint
- [ ] Add s3 caching Option
- [ ] The Output Registry should warn when a target is overwriting another's outputs
- [ ] Logging: Create a diagnotics module that we attach to the context and that - on failure - will always write all the diagnostics to the grog root
- [ ] Docs: Strong README
- [ ] Execution: Warn if one or more of the input files specified for a target do not exist
  - Make this an option that can also error
- [ ] Record a brief terminal clip using tea's vhs tape
- [ ] Support running multiple targets with `grog run` at the same time
- [ ] Tests: Use the new synctest package to better test the execution semantic
