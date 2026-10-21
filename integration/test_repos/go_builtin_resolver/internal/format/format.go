package format

import _ "embed"

//go:embed templates/hello.txt
var template string

func Render(name string) string { return template + name }
