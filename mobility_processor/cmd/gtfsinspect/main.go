// gtfsinspect reads local files and private temporary spools only. It cannot write a database.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"mobility_processor/internal/gtfs/parser"
	"os"
	"runtime"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	dir := flag.String("data-dir", "", "local GTFS directory")
	maxRows := flag.Int("max-rows", 2000000, "validation row budget")
	chunk := flag.Int("chunk-rows", 4096, "external sort memory bound")
	temp := flag.String("temp-dir", "", "parent for private temporary files")
	legacy := flag.Bool("measure-legacy", false, "measure bounded old reader; expected budget failure is reported")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "--data-dir required")
		return 1
	}
	start := time.Now()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	if *legacy {
		_, err := parser.LoadContext(context.Background(), *dir, *maxRows)
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		message := ""
		if err != nil {
			message = err.Error()
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "bounded_legacy_measurement", "max_rows": *maxRows, "elapsed_seconds": time.Since(start).Seconds(), "heap_alloc_bytes": after.HeapAlloc, "allocated_bytes": after.TotalAlloc - before.TotalAlloc, "error": message})
		return 0
	}
	p, err := parser.Prepare(context.Background(), *dir, parser.PrepareOptions{MaxRows: *maxRows, ChunkRows: *chunk, TempDir: *temp, CollectIssues: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer p.Close()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "read_only_stream_validation", "valid": len(p.Issues) == 0, "plan": p, "elapsed_seconds": time.Since(start).Seconds(), "heap_alloc_bytes": after.HeapAlloc, "allocated_bytes": after.TotalAlloc - before.TotalAlloc})

	if len(p.Issues) > 0 {
		return 2
	}
	return 0
}
