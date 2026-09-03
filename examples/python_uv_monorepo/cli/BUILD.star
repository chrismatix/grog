load("//tools/grog/python.star", "python_image", "python_library")

python_library(
    name = "cli",
    deps = ["//lib/format"],
)

python_image(
    name = "cli",
    library = ":cli",
    push = ["registry.example.com/sarcasm/cli:" + GROG_GIT_HASH],
)
