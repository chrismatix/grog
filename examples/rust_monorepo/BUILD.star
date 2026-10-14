# The cargo resolver reads the workspace manifests and synthesizes one
# :_cargo_package filegroup per crate, with the crate's path deps as edges.
dependency_resolver(
    name = "cargo",
    command = "builtin::cargo",
)

target(
    name = "workspace",
    inputs = [
        "Cargo.toml",
        "Cargo.lock",
    ],
)
