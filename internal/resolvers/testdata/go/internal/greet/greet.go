package greet

import "example.com/mono/internal/format"

func Hello() string { return format.Render("hello") }
