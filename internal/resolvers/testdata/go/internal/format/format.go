package format

import (
	"embed"
	_ "embed"
)

//go:embed templates/*.txt
var templates embed.FS

//go:embed "banner.txt" `static`
var banner string

//go:embed vendor/assets/logo.txt
var logo string

func Render(text string) string { return banner + text }
