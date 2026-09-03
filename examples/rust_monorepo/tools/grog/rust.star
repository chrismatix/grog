"""Grog targets for a cargo workspace. Starlark twin of rust.pkl."""

def cargo_crate(name, deps = [], bin = False):
    """Filegroup `name` plus `deps-lock`, `build`, `test` and `lint` targets for one crate.

    `deps` are the filegroups of the workspace crates this crate depends on.
    With `bin = True` the release binary is copied to bin/<name> and exposed as
    the `build` target's bin_output, so `grog run //crates/<name>:build` works.
    """
    target(
        name = name,
        inputs = ["src/**/*", "Cargo.toml"],
        dependencies = ["//tools/grog:rust"] + deps,
    )

    # This crate's slice of the shared Cargo.lock. Cargo targets depend on it
    # instead of //:workspace, so unrelated lockfile churn keeps them cached.
    target(
        name = "deps-lock",
        command = "cargo tree -p %s -e normal,build,dev --prefix none --locked | sed -E 's/ \\([^)]*\\)$//' | sort -u > deps.lock" % name,
        inputs = ["Cargo.toml"],
        dependencies = ["//:workspace"],
        outputs = ["deps.lock"],
    )

    cargo_deps = [":" + name, ":deps-lock"]

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
        inputs = ["tests/**/*"],
        dependencies = cargo_deps,
        concurrency_group = "cargo",
    )

    target(
        name = "lint",
        command = "cargo fmt -p %s --check && cargo clippy -p %s --all-targets --locked -- -D warnings" % (name, name),
        inputs = ["tests/**/*"],
        dependencies = cargo_deps,
        concurrency_group = "cargo",
    )
