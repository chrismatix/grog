load("//rules.star", "simple_package")

simple_package(
    name = "macro_example",
    srcs = ["*.txt"],
)
