package main

import (
	"fmt"

	"example.com/gen"
	shim "example.com/mono/internal/_shim"
	"example.com/mono/internal/greet"
	"github.com/other/lib"
)

func main() {
	fmt.Println(greet.Hello(), gen.Version, lib.Name, shim.Name)
}
