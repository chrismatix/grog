target(
    name = "bar",
    command = "cat source.txt ../foo/output.txt > output.txt && cat source.txt ../foo/output.txt > ../bar_output.txt",
    dependencies = ["//foo"],
    outputs = [
        "output.txt",
        "../bar_output.txt",
    ],
)

target(
    name = "bar_test",
    command = "cat output.txt && echo 'some text' >> output.txt && cp output.txt test_output.txt",
    dependencies = [":bar"],
    inputs = ["source.txt"],
    outputs = ["test_output.txt"],
)
