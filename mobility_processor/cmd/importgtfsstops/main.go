package main

import (
	"log"
	"mobility_processor/internal/gtfscli"
	"os"
)

// Historical command names now share the canonical PostgreSQL pipeline; no ZIP-to-cache identity inference.
func main() {
	if err := gtfscli.Run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}
