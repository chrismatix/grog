"""Grog targets for a cargo workspace. Starlark twin of rust.pkl."""

def cargo_crate(name, bin = False):
    """`deps-lock`, `build`, `test` and `lint` targets for one crate.

    Sources and cross-crate edges come from the `:_cargo_package` filegroup
    that the root `cargo` resolver synthesizes. With `bin = True` the release
    binary is copied to bin/<name> and exposed as the `build` target's
    bin_output, so `grog run //crates/<name>:build` works.
    """

    # This crate's slice of the shared Cargo.lock. Cargo targets depend on it
    # instead of //:workspace, so unrelated lockfile churn keeps them cached.
    target(
        name = "deps-lock",
        command = "cargo tree -p %s -e normal,build,dev --prefix none --locked | sed -E 's/ \\([^)]*\\)$//' | sort -u > deps.lock" % name,
        inputs = ["Cargo.toml"],
        dependencies = ["//:workspace"],
        outputs = ["deps.lock"],
    )

    cargo_deps = [":_cargo_package", ":deps-lock", "//tools/grog:rust"]

    build = dict(
        name = "build",
        command = "cargo build -p %s --release --locked" % name,
        dependencies = cargo_deps,
        concurrency_group = "cargo",
    )
    if bin:
        build["command"] += '\nmkdir -p bin && cp "$GROG_WORKSPACE_ROOT/target/release/%s" bin/%s' % (name, name)
        build["bin_output"] = "bin/" + name
    target(**build)

    target(
        name = "test",
        command = "cargo test -p %s --locked" % name,
        dependencies = cargo_deps,
        concurrency_group = "cargo",
    )

    target(
        name = "lint",
        command = "cargo fmt -p %s --check && cargo clippy -p %s --all-targets --locked -- -D warnings" % (name, name),
        dependencies = cargo_deps,
        concurrency_group = "cargo",
    )
