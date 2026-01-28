target(
    name = "foo",
    command = """
echo "Hello streamed output"
cat src/source_1.txt src/source_2.txt > output.txt
""",
    inputs = ["src/*.txt"],
    outputs = ["output.txt"],
)

target(
    name = "foo_test",
    command = """
mkdir -p dist
echo "Hello test streamed output"
cat src/source_1.txt > dist/test_output.txt
""",
    inputs = ["src/*.txt"],
    outputs = ["dist/test_output.txt"],
    dependencies = [":foo"],
)

alias(
    name = "foo_alias",
    actual = ":foo",
)
