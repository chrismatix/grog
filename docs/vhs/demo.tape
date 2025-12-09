Output demo.gif
Output demo.webm


Set Shell "bash"
Set Width 1200
Set Height 700
Set FontSize 22
Set Theme "Dracula"
Set Padding 16

Hide
Type "cd ../../examples/python_monorepo"
Enter
Type "git reset --hard"
Enter
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
Sleep 100ms
Type "# Our build graph."
Enter
Sleep 2000ms

Ctrl+L

Type "grog build"
Enter
Wait
Sleep 100ms
Type "# All cached all good!"
Enter
Sleep 2000ms
Ctrl+L

Type "# Let's simulate a partial change"
Enter
Type "echo '// Im a change!' >> lib/proto/schema/sarcasm.proto"
Enter

Sleep 500ms

Type "grog build"
Enter
Wait

Sleep 2000ms

Type "grog test"
Enter
Wait

Sleep 2000ms

Hide
# Reset the repo
Type "git reset --hard"
Enter
