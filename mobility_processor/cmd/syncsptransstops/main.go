package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"github.com/joho/godotenv"

	"mobility_processor/internal/sptrans"
	"mobility_processor/internal/stops"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	workers := flag.Int("workers", envInt("SPTRANS_STOPS_WORKERS", 20), "quantidade de workers paralelos")
	flag.Parse()

	token := os.Getenv("SPTRANS_API_TOKEN")
	if token == "" {
		log.Fatal("SPTRANS_API_TOKEN não definido")
	}

	repo, err := stops.OpenFromEnv()
	if err != nil {
		log.Fatalf("Erro ao abrir banco: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	if err := repo.EnsureSchema(ctx); err != nil {
		log.Fatalf("Banco não preparado: %v", err)
	}

	client, err := sptrans.NewClient(token)
	if err != nil {
		log.Fatalf("Erro ao criar cliente SPTrans: %v", err)
	}

	if err := client.Authenticate(); err != nil {
		log.Fatalf("Falha na autenticação SPTrans: %v", err)
	}

	log.Printf("Sincronizando pontos da SPTrans com %d workers...", *workers)

	var persisted atomic.Int64

	result, err := client.GetAllStops(*workers, func(batch []sptrans.Stop) {
		for _, stop := range batch {
			if err := repo.UpsertStop(ctx, stop); err != nil {
				log.Printf("ERRO: %v", err)
				continue
			}
			persisted.Add(1)
		}
		log.Printf("+%d pontos novos/atualizados (total parcial: %d)", len(batch), persisted.Load())
	})
	if err != nil {
		log.Fatalf("Erro ao buscar pontos: %v", err)
	}

	var linked atomic.Int64
	for _, link := range result.LineStops {
		inserted, err := repo.UpsertLineStop(ctx, link)
		if err != nil {
			log.Printf("ERRO: %v", err)
			continue
		}
		linked.Add(inserted)
	}

	totalStops, err := repo.Count(ctx)
	if err != nil {
		log.Printf("Não foi possível contar pontos no banco: %v", err)
	}
	totalLinks, err := repo.CountLinks(ctx)
	if err != nil {
		log.Printf("Não foi possível contar vínculos no banco: %v", err)
	}

	log.Println("════════════════════════════════════════")
	log.Printf("  Linhas processadas        : %d", result.TotalLines)
	log.Printf("  Pontos únicos recebidos   : %d", result.Total)
	log.Printf("  Pontos salvos/atualizados : %d", persisted.Load())
	log.Printf("  Vínculos recebidos        : %d", result.TotalLinks)
	log.Printf("  Vínculos novos inseridos  : %d", linked.Load())
	log.Printf("  Erros de request          : %d", result.Errors)
	if totalStops > 0 {
		log.Printf("  Total de pontos no banco  : %d", totalStops)
	}
	if totalLinks > 0 {
		log.Printf("  Total vínculos no banco   : %d", totalLinks)
	}
	log.Printf("  Tempo total               : %s", result.Elapsed)
	log.Println("════════════════════════════════════════")
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
