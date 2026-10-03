load("//tools/grog/python.star", "python_image", "python_package")

python_package(name = "cli")

python_image(
    name = "cli",
    push = ["registry.example.com/sarcasm/cli:" + GROG_GIT_HASH],
)
