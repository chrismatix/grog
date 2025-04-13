package main

import (
	"fmt"
	"github.com/spf13/cobra/doc"
	"grog/internal/cmd"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func main() {
	// Ensure output directory exists
	if err := os.MkdirAll("docs", 0755); err != nil {
		log.Fatalf("failed to create docs dir: %v", err)
	}

	const fmTemplate = `---
title: "%s"
---
`

	filePrepender := func(filename string) string {
		name := filepath.Base(filename)
		base := strings.TrimSuffix(name, path.Ext(name))
		return fmt.Sprintf(fmTemplate, strings.Replace(base, "_", " ", -1))
	}

	linkHandler := func(name string) string {
		base := strings.TrimSuffix(name, path.Ext(name))
		return "/reference/cli/" + strings.ToLower(base) + "/"
	}

	err := doc.GenMarkdownTreeCustom(cmd.RootCmd, "./src/content/docs/reference/cli", filePrepender, linkHandler)
	if err != nil {
		log.Fatalf("failed to generate docs: %v", err)
	}

	log.Println("Docs generated successfully.")
}
