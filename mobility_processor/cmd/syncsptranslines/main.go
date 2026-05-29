package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/joho/godotenv"

	"mobility_processor/internal/lines"
	"mobility_processor/internal/sptrans"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	termsFlag := flag.String("terms", env("SPTRANS_SYNC_TERMS", "0,1,2,3,4,5,6,7,8,9"), "termos separados por vírgula para /Linha/Buscar")
	workers := flag.Int("workers", envInt("SPTRANS_SYNC_WORKERS", 10), "quantidade de workers paralelos")
	flag.Parse()

	token := os.Getenv("SPTRANS_API_TOKEN")
	if token == "" {
		log.Fatal("SPTRANS_API_TOKEN não definido")
	}

	repo, err := lines.OpenFromEnv()
	if err != nil {
		log.Fatalf("Erro ao abrir banco: %v", err)
	}
	defer repo.Close()

	client, err := sptrans.NewClient(token)
	if err != nil {
		log.Fatalf("Erro ao criar cliente SPTrans: %v", err)
	}

	if err := client.Authenticate(); err != nil {
		log.Fatalf("Falha na autenticação SPTrans: %v", err)
	}

	terms := splitCSV(*termsFlag)
	log.Printf("Sincronizando linhas SPTrans com %d termos e %d workers...", len(terms), *workers)

	ctx := context.Background()
	var persisted atomic.Int64
	result := client.SyncLines(terms, *workers, func(raw sptrans.RawLine) error {
		if err := repo.UpsertFromSPTrans(ctx, raw); err != nil {
			return err
		}
		persisted.Add(1)
		return nil
	})

	total, err := repo.Count(ctx)
	if err != nil {
		log.Printf("Não foi possível contar linhas no banco: %v", err)
	}

	log.Println("════════════════════════════════════════")
	log.Printf("  Registros recebidos : %d", result.Total)
	log.Printf("  Upserts concluídos  : %d", persisted.Load())
	log.Printf("  Erros               : %d", len(result.Errors))
	if total > 0 {
		log.Printf("  Total no banco      : %d", total)
	}
	log.Println("════════════════════════════════════════")

	for _, err := range result.Errors {
		log.Printf("ERRO: %v", err)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		var parsed int
		if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}
