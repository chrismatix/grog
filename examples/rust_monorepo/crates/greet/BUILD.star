load("//tools/grog/rust.star", "cargo_crate")

cargo_crate(
    name = "greet",
    deps = ["//crates/format"],
)
