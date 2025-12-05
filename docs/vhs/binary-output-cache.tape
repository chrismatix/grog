Output docs/public/vhs/binary-output-cache.gif
Output docs/public/vhs/binary-output-cache.webm

Set Shell "bash"
Set Width 1200
Set Height 700
Set FontSize 22
Set Theme "Dracula"
Set TypingSpeed 30ms
Set Padding 16

Type "cd integration/test_repos/binary_output"
Enter
Sleep 500ms

Type "../../../dist/grog build //..."
Enter
Sleep 2000ms

Type "echo \"\" >> bin_tool.sh"
Enter
Sleep 500ms

Type "../../../dist/grog build //..."
Enter
Sleep 2000ms
