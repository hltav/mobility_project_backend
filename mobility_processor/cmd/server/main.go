package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"mobility_processor/internal/api"
	"mobility_processor/internal/sptrans"
)

// loadEnv lê um arquivo .env e seta as variáveis no processo,
// sem sobrescrever variáveis já definidas no ambiente.
func loadEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // arquivo não existe, sem problema
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// ignora comentários e linhas vazias
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)

		// só seta se ainda não estiver definida
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func main() {
	// Sobe dois níveis: cmd/server/main.go → raiz do projeto
	godotenv.Load("../.env")

	token := os.Getenv("SPTRANS_API_TOKEN")
	if token == "" {
		log.Fatal("SPTRANS_API_TOKEN não definido")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	client, err := sptrans.NewClient(token)
	if err != nil {
		log.Fatalf("Erro ao criar cliente SPTrans: %v", err)
	}

	mux := http.NewServeMux()
	handler := api.NewHandler(client)
	handler.RegisterRoutes(mux)

	log.Printf("[mobility-processor] ouvindo em :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
