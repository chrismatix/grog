//go:build ignore

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Printf("arguments: %q\n", os.Args[1:])
}
