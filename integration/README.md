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

## Windows and QEMU

The Windows workflow builds the release executable and runs unit tests plus `TestWindowsWorkflow`. The latter covers nested packages, paths with spaces, resources, cache restoration, tainting, native executables, scripts, traces, and timeouts. The existing snapshot and Unix PTY tests remain in the Linux suite.

On Windows x64, install Go, Git for Windows, PKL, and [WinLibs GCC 16.2.0 (POSIX, SEH, UCRT)](https://github.com/brechtsanders/winlibs_mingw/releases/tag/16.2.0posix-14.0.0-ucrt-r1), matching the compiler pinned in the Windows workflow. Add Go, Git, PKL, and the extracted `mingw64\bin` directory to `PATH`, then run from a checkout:

```powershell
./.github/release-windows.ps1
Copy-Item dist/grog-windows-amd64.exe dist/grog.exe
go test ./internal/...
go test ./integration/... -run TestWindows -v
```

The prebuilt DuckDB library requires emulated TLS symbols that [MSYS2's GCC 16 runtime no longer provides](https://www.msys2.org/news/#2026-05-11-native-thread-local-storage-tls-with-gcc-16); use the pinned WinLibs compiler for both builds and tests.

For local Linux verification, [Dockur](https://github.com/dockur/windows) provisions a Windows evaluation guest using QEMU/KVM:

```sh
vm_directory=$(mktemp -d)
docker run --name grog-windows --device=/dev/kvm --device=/dev/net/tun \
  --cap-add NET_ADMIN -e VERSION=2022 -e RAM_SIZE=8G -e CPU_CORES=8 \
  -p 127.0.0.1:8006:8006 -v "$vm_directory:/storage" \
  -v "$PWD:/shared" --stop-timeout 60 dockurr/windows
```

The guest can access the checkout at `\\host.lan\Data`. Copy it to a local Windows directory before building; exclude the `.git` file when copying a Linux worktree, then initialize a Git repository for release metadata. Run the same PowerShell commands above. Stop the container after testing; the guest disk remains in `vm_directory` for reuse.

## Gated scenarios

Scenarios that talk to a cloud registry/cache (e.g. GAR) are skipped
unless `REQUIRES_CREDENTIALS=1` is set, so the default `make test` run
does not depend on ambient cloud credentials.
