package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"mobility_processor/internal/geocoding"
	"mobility_processor/internal/lines"
)

type Line struct {
	ID        int    `json:"id"`
	Origin    string `json:"origin"`
	SptransID int    `json:"sptrans_id,omitempty"`
}

type GeocodeResult struct {
	ID  int     `json:"id"`
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func loadEnv(path string) {
	// Tenta o caminho explícito primeiro, depois procura no diretório atual.
	paths := []string{path, "./.env"}
	for _, p := range paths {
		if p == "" {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
		return
	}
}

func fetchLines(filePath, laravelURL string) ([]Line, error) {
	if filePath != "" {
		return loadLinesFromFile(filePath)
	}
	if laravelURL != "" {
		return fetchLinesFromLaravel(laravelURL)
	}
	return nil, fmt.Errorf("nenhuma fonte de linhas configurada: defina LINES_JSON_PATH ou LARAVEL_URL")
}

func loadLinesFromFile(path string) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lines file: %w", err)
	}

	var direct []Line
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}

	var paginated struct {
		Data []Line `json:"data"`
	}
	if err := json.Unmarshal(data, &paginated); err == nil {
		return paginated.Data, nil
	}

	return nil, fmt.Errorf("arquivo %s não pôde ser decodificado como JSON de linhas", path)
}

func fetchLinesFromLaravel(laravelURL string) ([]Line, error) {
	resp, err := http.Get(strings.TrimRight(laravelURL, "/") + "/api/internal/lines/without-coordinates")
	if err != nil {
		return nil, fmt.Errorf("fetch lines: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var lines []Line
	if err := json.Unmarshal(body, &lines); err == nil {
		return lines, nil
	}

	var paginated struct {
		Data []Line `json:"data"`
	}
	if err := json.Unmarshal(body, &paginated); err == nil {
		return paginated.Data, nil
	}

	return nil, fmt.Errorf("decode lines: %w", err)
}

func saveResults(laravelURL, outputPath string, results []GeocodeResult) error {
	if outputPath != "" {
		return writeResultsToFile(outputPath, results)
	}
	if laravelURL != "" {
		return postResultsToLaravel(laravelURL, results)
	}
	return nil
}

func writeResultsToFile(path string, results []GeocodeResult) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write results file: %w", err)
	}
	return nil
}

func postResultsToLaravel(laravelURL string, results []GeocodeResult) error {
	body, err := json.Marshal(map[string]any{"results": results})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := http.Post(strings.TrimRight(laravelURL, "/")+"/api/internal/lines/update-coordinates", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("save results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("laravel returned status %d", resp.StatusCode)
	}
	return nil
}

func main() {
	loadEnv("../.env")

	fresh := flag.Bool("fresh", envBool("GEOCODE_FRESH", false), "regeocodifica todas as linhas, inclusive as que já têm coordenadas")
	limit := flag.Int("limit", envInt("GEOCODE_LIMIT", 0), "limite opcional de linhas para geocodificar")
	flag.Parse()

	laravelURL := os.Getenv("LARAVEL_URL")
	linesFile := os.Getenv("LINES_JSON_PATH")
	outputFile := os.Getenv("OUTPUT_FILE")

	if canUseDatabase() && linesFile == "" {
		if err := geocodeDatabase(*fresh, *limit); err != nil {
			log.Fatalf("Erro ao geocodificar linhas no banco: %v", err)
		}
		return
	}

	lines, err := fetchLines(linesFile, laravelURL)
	if err != nil {
		log.Fatalf("Erro ao buscar linhas: %v", err)
	}

	if len(lines) == 0 {
		log.Println("Nenhuma linha para geocodificar.")
		return
	}

	log.Printf("%d linhas para geocodificar", len(lines))
	log.Println("Rate limit: 1 req/s (Nominatim)")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var success atomic.Int64
	var failed atomic.Int64
	var results []GeocodeResult

	for i, line := range lines {
		<-ticker.C

		coords, err := geocoding.Geocode(line.Origin)
		if err != nil {
			log.Printf("[%d/%d] ERRO %q: %v", i+1, len(lines), line.Origin, err)
			failed.Add(1)
			continue
		}

		if coords == nil {
			log.Printf("[%d/%d] SEM RESULTADO %q", i+1, len(lines), line.Origin)
			failed.Add(1)
			continue
		}

		success.Add(1)
		results = append(results, GeocodeResult{ID: line.ID, Lat: coords.Lat, Lng: coords.Lng})
		log.Printf("[%d/%d] ✓ %q → %.6f, %.6f", i+1, len(lines), line.Origin, coords.Lat, coords.Lng)
	}

	if err := saveResults(laravelURL, outputFile, results); err != nil {
		log.Printf("Falha ao salvar resultados: %v", err)
	} else if outputFile != "" {
		log.Printf("Resultados salvos em %s", outputFile)
	}

	log.Println("════════════════════════════════════════")
	log.Printf("  Total processado : %d", len(lines))
	log.Printf("  Geocodificadas   : %d", success.Load())
	log.Printf("  Sem resultado    : %d", failed.Load())
	log.Println("════════════════════════════════════════")
}

func geocodeDatabase(fresh bool, limit int) error {
	repo, err := lines.OpenFromEnv()
	if err != nil {
		return err
	}
	defer repo.Close()

	ctx := context.Background()
	dbLines, err := repo.LinesForGeocoding(ctx, fresh, limit)
	if err != nil {
		return err
	}
	if len(dbLines) == 0 {
		log.Println("Todas as linhas já estão geocodificadas.")
		return nil
	}

	origins := uniqueOrigins(dbLines)
	log.Printf("%d linhas para geocodificar no banco (%d origens únicas)", len(dbLines), len(origins))
	log.Println("Rate limit: 1 req/s (Nominatim)")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var success atomic.Int64
	var failed atomic.Int64

	for i, origin := range origins {
		<-ticker.C

		coords, err := geocoding.Geocode(origin)
		if err != nil {
			log.Printf("[%d/%d] ERRO %q: %v", i+1, len(origins), origin, err)
			failed.Add(1)
			continue
		}
		if coords == nil {
			log.Printf("[%d/%d] SEM RESULTADO %q", i+1, len(origins), origin)
			failed.Add(1)
			continue
		}
		updated, err := repo.UpdateOriginCoordinatesByOrigin(ctx, origin, coords.Lat, coords.Lng)
		if err != nil {
			log.Printf("[%d/%d] ERRO AO SALVAR %q: %v", i+1, len(origins), origin, err)
			failed.Add(1)
			continue
		}

		success.Add(updated)
		log.Printf("[%d/%d] ✓ %q → %.6f, %.6f (%d linhas)", i+1, len(origins), origin, coords.Lat, coords.Lng, updated)
	}

	log.Println("════════════════════════════════════════")
	log.Printf("  Total processado : %d", len(dbLines))
	log.Printf("  Geocodificadas   : %d", success.Load())
	log.Printf("  Sem resultado    : %d origens", failed.Load())
	log.Println("════════════════════════════════════════")
	return nil
}

func uniqueOrigins(dbLines []lines.Line) []string {
	seen := make(map[string]struct{}, len(dbLines))
	origins := make([]string, 0, len(dbLines))
	for _, line := range dbLines {
		origin := strings.TrimSpace(line.Origin)
		if origin == "" {
			continue
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins
}

func canUseDatabase() bool {
	return os.Getenv("DB_DSN") != "" || (os.Getenv("DB_DATABASE") != "" && os.Getenv("DB_USERNAME") != "")
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "sim"
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
