load("//tools/grog/rust.star", "cargo_crate")

cargo_crate(
    name = "cli",
    bin = True,
    deps = ["//crates/greet"],
)
