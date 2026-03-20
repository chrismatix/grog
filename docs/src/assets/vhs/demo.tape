Output demo.gif
Output demo.webm


Set Shell "bash"
Set Width 1200
Set Height 700
Set FontSize 22
Set Theme "Dracula"
Set Padding 16

Hide
Type "cd ../../../examples/python_monorepo"
Enter
Type "git reset --hard"
Enter

# Clean to ensure that the dummy change is not cached
Type "grog clean"
Enter
Wait

# Pre-build the app once
Type "grog build"
Enter
Wait

Type "grog test"
Enter
Wait

Ctrl+L
Show

Type "grog graph"
Enter

Sleep 300ms
Type "# Our build graph."
Enter
Sleep 2000ms

Ctrl+L

Type "grog build"
Enter
Wait

Sleep 2000ms

Type "grog test"
Enter
Wait

Sleep 300ms

Type "# All cached all good!"
Enter
Sleep 2000ms
Ctrl+L

Type "# Let's simulate a change to a dependency."
Enter
Sleep 200ms
Type "echo '# Im a change!' >> lib/format/format/__init__.py"
Enter

Sleep 500ms

Type "grog build"
Enter
Wait


Sleep 2000ms

Hide
# Reset the repo
Type "git reset --hard"
Enter
