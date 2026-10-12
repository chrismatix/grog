package main

import (
	"fmt"

	"example.com/gen"
	"example.com/mono/internal/greet"
	"github.com/other/lib"
)

func main() {
	fmt.Println(greet.Hello(), gen.Version, lib.Name)
}
