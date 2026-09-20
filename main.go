package main

import (
	"codex-ritalin/internal/ritalin"
	"fmt"
	"os"
)

func main() {
	code, err := ritalin.Run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-ritalin:", err)
	}
	os.Exit(code)
}
