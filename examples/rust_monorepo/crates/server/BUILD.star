load("//tools/grog/rust.star", "cargo_crate")

cargo_crate(
    name = "server",
    bin = True,
)

# The docker context is the workspace root so the path dependencies resolve
# inside the builder stage. :_cargo_package covers them transitively.
target(
    name = "image",
    command = "docker build --platform=linux/amd64 -f Dockerfile -t rust-monorepo-server ../..",
    dependencies = [
        ":_cargo_package",
        "//:workspace",
    ],
    inputs = ["Dockerfile"],
    oci_push = {"rust-monorepo-server": ["registry.example.com/rust-monorepo/server:" + GROG_GIT_HASH]},
    outputs = ["oci::rust-monorepo-server"],
    platforms = ["linux/amd64"],
)
