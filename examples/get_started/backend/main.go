package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	dir := os.Getenv("STATIC_DIR")
	if dir == "" {
		dir = "./frontend/dist"
	}

	fs := http.FileServer(http.Dir(dir))
	http.Handle("/", fs)

	log.Printf("Serving %s on :8080\n", dir)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
