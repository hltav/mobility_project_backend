package main

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/joho/godotenv"

	"mobility_processor/internal/stops"
)

func main() {
	_ = godotenv.Load(".env", "../.env")

	input := flag.String("file", "", "caminho para GTFS .zip, pasta GTFS, ou stops.txt")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		log.Fatal("Informe -file com o caminho do GTFS .zip, pasta GTFS, ou stops.txt")
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

	reader, closer, err := openStopsFile(*input)
	if err != nil {
		log.Fatalf("Erro ao abrir stops.txt: %v", err)
	}
	defer closer()

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1

	header, err := csvReader.Read()
	if err != nil {
		log.Fatalf("Erro ao ler cabeçalho do stops.txt: %v", err)
	}

	index := headerIndex(header)
	required := []string{"stop_id", "stop_name", "stop_lat", "stop_lon"}
	for _, field := range required {
		if _, ok := index[field]; !ok {
			log.Fatalf("stops.txt sem coluna obrigatória %q", field)
		}
	}

	var imported atomic.Int64
	var skipped atomic.Int64

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Linha ignorada por erro de CSV: %v", err)
			skipped.Add(1)
			continue
		}

		stop, ok := parseGTFSStop(record, index)
		if !ok {
			skipped.Add(1)
			continue
		}

		if err := repo.UpsertGTFSStop(ctx, stop); err != nil {
			log.Printf("ERRO: %v", err)
			skipped.Add(1)
			continue
		}
		imported.Add(1)
	}

	totalStops, err := repo.Count(ctx)
	if err != nil {
		log.Printf("Não foi possível contar pontos no banco: %v", err)
	}

	log.Println("════════════════════════════════════════")
	log.Printf("  Pontos GTFS importados : %d", imported.Load())
	log.Printf("  Registros ignorados    : %d", skipped.Load())
	if totalStops > 0 {
		log.Printf("  Total de pontos banco  : %d", totalStops)
	}
	log.Println("════════════════════════════════════════")
}

func openStopsFile(input string) (io.Reader, func() error, error) {
	info, err := os.Stat(input)
	if err != nil {
		return nil, nil, err
	}

	if info.IsDir() {
		f, err := os.Open(filepath.Join(input, "stops.txt"))
		if err != nil {
			return nil, nil, err
		}
		return f, f.Close, nil
	}

	if strings.EqualFold(filepath.Ext(input), ".zip") {
		zr, err := zip.OpenReader(input)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range zr.File {
			if strings.EqualFold(filepath.Base(f.Name), "stops.txt") {
				rc, err := f.Open()
				if err != nil {
					zr.Close()
					return nil, nil, err
				}
				return rc, func() error {
					err1 := rc.Close()
					err2 := zr.Close()
					if err1 != nil {
						return err1
					}
					return err2
				}, nil
			}
		}
		zr.Close()
		return nil, nil, fmt.Errorf("stops.txt não encontrado dentro do zip")
	}

	f, err := os.Open(input)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}

func headerIndex(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, field := range header {
		index[strings.TrimSpace(field)] = i
	}
	return index
}

func parseGTFSStop(record []string, index map[string]int) (stops.GTFSStop, bool) {
	codeValue := value(record, index, "stop_code")
	if codeValue == "" {
		codeValue = value(record, index, "stop_id")
	}

	code, err := strconv.Atoi(strings.TrimSpace(codeValue))
	if err != nil {
		return stops.GTFSStop{}, false
	}

	lat, err := strconv.ParseFloat(strings.TrimSpace(value(record, index, "stop_lat")), 64)
	if err != nil {
		return stops.GTFSStop{}, false
	}
	lng, err := strconv.ParseFloat(strings.TrimSpace(value(record, index, "stop_lon")), 64)
	if err != nil {
		return stops.GTFSStop{}, false
	}

	address := value(record, index, "stop_desc")
	return stops.GTFSStop{
		Code:    code,
		Name:    value(record, index, "stop_name"),
		Address: address,
		Lat:     lat,
		Lng:     lng,
	}, true
}

func value(record []string, index map[string]int, field string) string {
	i, ok := index[field]
	if !ok || i < 0 || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}
