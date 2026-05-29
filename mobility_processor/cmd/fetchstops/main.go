package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"mobility_processor/internal/sptrans"
)

func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
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
}

func main() {
	loadEnv("../.env")

	token := os.Getenv("SPTRANS_API_TOKEN")
	if token == "" {
		log.Fatal("SPTRANS_API_TOKEN não definido")
	}

	client, err := sptrans.NewClient(token)
	if err != nil {
		log.Fatalf("Erro ao criar cliente: %v", err)
	}

	if err := client.Authenticate(); err != nil {
		log.Fatalf("Falha na autenticação: %v", err)
	}

	const workers = 30

	log.Printf("Iniciando varredura em 2 estágios com %d workers...", workers)

	var total atomic.Int64

	result, err := client.GetAllStops(workers, func(batch []sptrans.Stop) {
		n := total.Add(int64(len(batch)))

		for _, s := range batch {
			fmt.Printf("CP=%-10d | %-50s | lat=%.6f lng=%.6f\n",
				s.CP, s.NP, s.Lat, s.Lng)
		}

		if n%1000 < int64(len(batch)) {
			log.Printf(">>> %d paradas únicas até agora...", n)
		}
	})

	if err != nil {
		log.Fatalf("Erro fatal: %v", err)
	}

	fmt.Println()
	log.Println("════════════════════════════════════════")
	log.Printf("  Linhas processadas      : %d", result.TotalLines)
	log.Printf("  Total de paradas únicas : %d", result.Total)
	log.Printf("  Erros de request        : %d", result.Errors)
	log.Printf("  Tempo total             : %s", result.Elapsed)
	log.Println("════════════════════════════════════════")
}
