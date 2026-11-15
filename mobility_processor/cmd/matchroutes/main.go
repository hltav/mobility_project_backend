package main

import (
	"context"
	"flag"
	"log"
	"mobility_processor/internal/database"
	"mobility_processor/internal/matching"
	"time"
)

func main() {
	feed := flag.Int64("feed-version", 0, "explicit ready feed version")
	confirm := flag.Bool("confirm-write", false, "save pending suggestions only")
	flag.Parse()
	if *feed <= 0 || !*confirm {
		log.Fatal("--feed-version and --confirm-write required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := database.OpenFromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	n, err := matching.NewMatcher(matching.NewRepository(db, *feed)).RunAll(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d pending suggestions processed", n)
}
