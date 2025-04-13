# Integration Testing

The tests in this repository are based on the cli binary testing outlined in [this article](https://lucapette.me/writing/writing-integration-tests-for-a-go-cli-application/) by Luca Pette.
The core idea is to test the real thing by first building the binary and then running it against a test repository defined in `test_repos`.

Because caching as well as listening for changes is a very important aspect of how grog works, we run `grog clean` on each repository for each test scenario defined in `test_scenarios`.
The tests are then run in the order they were defined in their test table and the output is compared against fixtures in `fixtures` where the file name is the same as the name of the test case.

**Note:** Test case names must be unique!

Example test scenario:

```yaml
name: simple json builds
cases:
  # First the cache is cleaned
  # Then this test is run
  - name: build_only_foo
    args:
      - build
      - //foo
    repo: simple_json

  # Then this test is run and the output should
  # reflect that foo was already built
  - name: build_bar_should_also_build_foo
    args:
      - build
      - //bar
    repo: simple_json
```

## Running tests

Run the integration tests with `make test`.

To update a single test fixture you can run `make test update={test case name}`
