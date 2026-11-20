# Filegroup every generated target depends on, so template edits re-run them.
target(
    name = "rust",
    inputs = [
        "rust.star",
        "rust.pkl",
    ],
)
