# Workspace manifest and lockfile. Crates derive their pruned deps-lock from it
# instead of depending on the whole lockfile.
target(
    name = "workspace",
    inputs = [
        "Cargo.toml",
        "Cargo.lock",
    ],
)
