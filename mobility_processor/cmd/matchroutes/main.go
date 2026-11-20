package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"mobility_processor/internal/database"
	"mobility_processor/internal/matching"
	"os"
	"time"
)

func main() {
	feed := flag.Int64("feed-version", 0, "explicit ready feed version")
	confirm := flag.Bool("confirm-write", false, "save pending suggestions only")
	dryRun := flag.Bool("dry-run", false, "read-only evidence report; never save suggestions")
	lineID := flag.Int64("line-id", 0, "internal operational line ID (dry-run only; zero means all)")
	flag.Parse()
	if *feed <= 0 || *dryRun == *confirm || *lineID < 0 || (!*dryRun && *lineID != 0) {
		log.Fatal("require --feed-version and exactly one of --dry-run / --confirm-write; --line-id is dry-run only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := database.OpenFromEnv(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	repo := matching.NewRepository(db, *feed)
	matcher := matching.NewMatcher(repo)
	if *dryRun {
		lines, candidates, err := repo.Snapshot(ctx, *lineID)
		if err != nil {
			log.Fatal("read-only matching snapshot failed")
		}
		report := make([]matching.Assessment, 0)
		operationalLines := make([]map[string]any, 0, len(lines))
		comparisonIDs := make([]int64, 0, len(lines))
		for _, line := range lines {
			comparisonIDs = append(comparisonIDs, line.LineID)
			if *lineID > 0 && line.LineID != *lineID {
				continue
			}
			var direction *int64
			if line.Direction.Valid {
				d := line.Direction.Int64
				direction = &d
			}
			operationalLines = append(operationalLines, map[string]any{"id": line.LineID, "code": line.Code, "cl": line.SptransLineID,
				"sptrans_direction": direction, "provider": line.Provider, "is_circular": line.Circular,
				"headsign": line.HeadSign, "tp": line.Origin, "ts": line.SecondaryLabel, "operational_stops": len(line.Stops)})
		}
		for _, assessment := range matcher.AssessBatch(lines, candidates) {
			if *lineID == 0 || assessment.LineID == *lineID {
				report = append(report, assessment)
			}
		}
		reportStatus := "pending_review"
		if len(report) == 0 {
			reportStatus = "no_match"
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "dry_run", "feed_version_id": *feed,
			"line_id": *lineID, "comparison_line_ids": comparisonIDs, "feed_scope": "explicit_ready_feed", "operational_lines": operationalLines, "status": reportStatus, "assessments": report}); err != nil {
			log.Fatal("encode report failed")
		}
		return
	}
	n, err := matcher.RunAll(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d pending suggestions processed", n)
}
