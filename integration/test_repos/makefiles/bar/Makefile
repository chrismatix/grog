# @grog
# inputs:
# - source.txt
# deps:
# - //foo:foo
# outputs:
# - output.txt
# - ../bar_output.txt
bar:
	cat source.txt ../foo/output.txt > output.txt
	cat source.txt ../foo/output.txt > ../bar_output.txt

# @grog
# inputs:
# - source.txt
# deps:
# - :bar
# outputs:
# - test_output.txt
bar_test:
	cat output.txt && echo 'some text' >> output.txt
	cp output.txt test_output.txt
