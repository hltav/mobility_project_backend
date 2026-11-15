package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"

	"mobility_processor/internal/api"
	"mobility_processor/internal/database"
	"mobility_processor/internal/lines"
	"mobility_processor/internal/sptrans"
	"mobility_processor/internal/stops"
	cache "mobility_processor/internal/valkey_cache"
)

func main() {
	godotenv.Load("../.env")

	token := os.Getenv("SPTRANS_API_TOKEN")
	if token == "" {
		log.Fatal("SPTRANS_API_TOKEN não definido")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	sptransClient, err := sptrans.NewClient(token)
	if err != nil {
		log.Fatalf("Erro ao criar cliente SPTrans: %v", err)
	}

	valkeyClient, err := cache.NewClientFromEnv()
	if err != nil {
		log.Fatalf("Erro ao conectar no Valkey: %v", err)
	}
	defer valkeyClient.Close()

	db, err := database.OpenFromEnv(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.CheckSchema(context.Background(), db); err != nil {
		log.Fatal(err)
	}
	lineRepo := lines.NewRepository(db)
	stopRepo := stops.NewRepository(db)

	mux := http.NewServeMux()
	handler := api.NewHandler(sptransClient, lineRepo, stopRepo, valkeyClient)
	handler.RegisterRoutes(mux)

	log.Printf("[mobility-processor] ouvindo em :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
