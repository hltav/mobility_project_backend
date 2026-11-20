package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"mobility_processor/internal/api"
	"mobility_processor/internal/database"
	"mobility_processor/internal/lines"
	"mobility_processor/internal/predictions"
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
	handler.SetCatalogAdminToken(os.Getenv("PROCESSOR_ADMIN_TOKEN"))
	if err := handler.SetStopSyncAllowedCLs(os.Getenv("PROCESSOR_STOPS_ALLOWED_CLS")); err != nil {
		log.Print("[stops sync] disabled: invalid explicit CL allowlist")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var workerDone chan struct{}
	defer func() {
		stop()
		if workerDone != nil {
			<-workerDone
		}
	}()
	if predictions.CollectionEnabled(os.Getenv("SPTRANS_PREDICTIONS_ENABLED")) {
		snapshots, cacheErr := cache.NewPredictionCache(valkeyClient, cache.PredictionCacheOptions{})
		if cacheErr != nil {
			log.Print("[predictions] disabled: invalid cache configuration")
		} else {
			collector, collectorErr := predictions.NewCollector(sptransClient, snapshots, valkeyClient, predictions.Options{MaxConcurrent: 1})
			if collectorErr != nil {
				log.Print("[predictions] disabled: invalid collector configuration")
			} else {
				worker, workerErr := predictions.NewDemandWorker(collector, lineRepo)
				if workerErr != nil {
					log.Print("[predictions] disabled: invalid worker configuration")
				} else {
					handler.SetPredictionDemandRecorder(worker)
					workerDone = make(chan struct{})
					go func() { defer close(workerDone); worker.Run(ctx) }()
					log.Print("[predictions] demand worker enabled")
				}
			}
		}
	}
	handler.RegisterRoutes(mux)

	log.Printf("[mobility-processor] ouvindo em :%s", port)
	server := &http.Server{Addr: ":" + port, Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Print("[mobility-processor] HTTP server stopped")
		}
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		_ = server.Close()
	}
}
