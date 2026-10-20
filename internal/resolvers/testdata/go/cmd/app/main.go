package main

import (
	"fmt"

	"example.com/gen"
	hidden "example.com/mono/.hidden/pkg"
	shim "example.com/mono/internal/_shim"
	"example.com/mono/internal/greet"
	"github.com/other/lib"
	local "old.example/lib/pkg"
)

func main() {
	fmt.Println(greet.Hello(), gen.Version, hidden.Name, lib.Name, local.Name, shim.Name)
}
