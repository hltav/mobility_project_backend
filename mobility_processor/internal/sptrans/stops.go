package sptrans

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"
)

// Stop representa uma parada de ônibus retornada pela API OlhoVivo.
type Stop struct {
	CP  int     `json:"cp"`
	NP  string  `json:"np"`
	Ed  string  `json:"ed"`
	Lat float64 `json:"py"`
	Lng float64 `json:"px"`
}

type LineStop struct {
	LineID   int
	StopCode int
}

// SearchStops busca paradas pelo termo (mantido para uso avulso).
func (c *Client) SearchStops(term string) ([]Stop, error) {
	q := url.Values{"termosBusca": {term}}
	body, err := c.get("/Parada/Buscar", q)
	if err != nil {
		return nil, err
	}
	var stops []Stop
	if err := json.Unmarshal(body, &stops); err != nil {
		return nil, fmt.Errorf("decode paradas (%q): %w", term, err)
	}
	return stops, nil
}

// getStopsByLine busca as paradas de uma linha específica.
func (c *Client) getStopsByLine(lineID int) ([]Stop, error) {
	q := url.Values{"codigoLinha": {fmt.Sprintf("%d", lineID)}}
	body, err := c.get("/Parada/BuscarParadasPorLinha", q)
	if err != nil {
		return nil, err
	}
	var stops []Stop
	if err := json.Unmarshal(body, &stops); err != nil {
		return nil, fmt.Errorf("decode paradas da linha %d: %w", lineID, err)
	}
	return stops, nil
}

// generateLineSearchTerms gera termos para varredura de linhas.
func generateLineSearchTerms() []string {
	terms := make([]string, 0, 36)
	for c := '0'; c <= '9'; c++ {
		terms = append(terms, string(c))
	}
	for c := 'a'; c <= 'z'; c++ {
		terms = append(terms, string(c))
	}
	return terms
}

// generateStopSearchTerms gera termos para varredura ampla de paradas.
func generateStopSearchTerms() []string {
	terms := make([]string, 0, 712)
	for c := '0'; c <= '9'; c++ {
		terms = append(terms, string(c))
	}
	for c := 'a'; c <= 'z'; c++ {
		terms = append(terms, string(c))
	}
	for c1 := 'a'; c1 <= 'z'; c1++ {
		for c2 := 'a'; c2 <= 'z'; c2++ {
			terms = append(terms, string(c1)+string(c2))
		}
	}
	return terms
}

// StopsFetchResult resume o resultado de GetAllStops.
type StopsFetchResult struct {
	Stops      []Stop
	LineStops  []LineStop
	Total      int
	TotalLinks int
	TotalLines int
	Errors     int
	Elapsed    time.Duration
}

// GetAllStops busca todas as paradas da cidade em dois estágios:
//  1. Varre a-z na /Linha/Buscar para coletar todas as linhas
//  2. Para cada linha, chama /Parada/BuscarParadasPorLinha em paralelo
//
// Deduplica paradas por CP. onProgress é chamado a cada batch (pode ser nil).
func (c *Client) GetAllStops(workers int, onProgress func(batch []Stop)) (StopsFetchResult, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return StopsFetchResult{}, err
	}

	start := time.Now()
	seenStops := make(map[int]struct{})
	var allStops []Stop
	errCount := 0

	// ── Estágio 0: coletar paradas por busca textual ────────────────────────
	log.Println("[Estágio 0] Buscando paradas por termos...")

	stopTerms := generateStopSearchTerms()
	stopTermsCh := make(chan string, len(stopTerms))
	for _, t := range stopTerms {
		stopTermsCh <- t
	}
	close(stopTermsCh)

	type stopSearchResult struct {
		term  string
		stops []Stop
		err   error
	}
	stopSearchCh := make(chan stopSearchResult, len(stopTerms))

	var wg0 sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg0.Add(1)
		go func() {
			defer wg0.Done()
			for term := range stopTermsCh {
				stops, err := c.SearchStops(term)
				stopSearchCh <- stopSearchResult{term: term, stops: stops, err: err}
			}
		}()
	}
	go func() { wg0.Wait(); close(stopSearchCh) }()

	termsHit := 0
	for r := range stopSearchCh {
		if r.err != nil {
			log.Printf("[Estágio 0] erro no termo %q: %v", r.term, r.err)
			errCount++
			continue
		}
		var batch []Stop
		for _, s := range r.stops {
			if _, ok := seenStops[s.CP]; !ok {
				seenStops[s.CP] = struct{}{}
				allStops = append(allStops, s)
				batch = append(batch, s)
			}
		}
		if len(batch) > 0 {
			termsHit++
			if onProgress != nil {
				onProgress(batch)
			}
		}
	}
	log.Printf("[Estágio 0] %d paradas únicas encontradas em %d termos com resultado", len(allStops), termsHit)

	// ── Estágio 1: coletar todas as linhas ──────────────────────────────────
	log.Println("[Estágio 1] Buscando todas as linhas...")

	terms := generateLineSearchTerms()
	termsCh := make(chan string, len(terms))
	for _, t := range terms {
		termsCh <- t
	}
	close(termsCh)

	type lineResult struct {
		lines []RawLine
		err   error
	}
	linesCh := make(chan lineResult, len(terms))

	var wg1 sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg1.Add(1)
		go func() {
			defer wg1.Done()
			for term := range termsCh {
				lines, err := c.SearchLines(term)
				linesCh <- lineResult{lines: lines, err: err}
			}
		}()
	}
	go func() { wg1.Wait(); close(linesCh) }()

	// Deduplica linhas por CL (sptrans_id)
	seenLines := make(map[int]struct{})
	var allLines []RawLine
	for r := range linesCh {
		if r.err != nil {
			log.Printf("[Estágio 1] erro: %v", r.err)
			continue
		}
		for _, l := range r.lines {
			if _, ok := seenLines[l.CL]; !ok {
				seenLines[l.CL] = struct{}{}
				allLines = append(allLines, l)
			}
		}
	}
	log.Printf("[Estágio 1] %d linhas únicas encontradas", len(allLines))

	// ── Estágio 2: buscar paradas por linha ─────────────────────────────────
	log.Println("[Estágio 2] Buscando paradas por linha...")

	lineIDsCh := make(chan int, len(allLines))
	for _, l := range allLines {
		lineIDsCh <- l.CL
	}
	close(lineIDsCh)

	type stopResult struct {
		lineID int
		stops  []Stop
		err    error
	}
	stopsCh := make(chan stopResult, len(allLines))

	var wg2 sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for lineID := range lineIDsCh {
				stops, err := c.getStopsByLine(lineID)
				stopsCh <- stopResult{lineID: lineID, stops: stops, err: err}
			}
		}()
	}
	go func() { wg2.Wait(); close(stopsCh) }()

	seenLineStops := make(map[LineStop]struct{})
	var allLineStops []LineStop

	for r := range stopsCh {
		if r.err != nil {
			log.Printf("[Estágio 2] erro: %v", r.err)
			errCount++
			continue
		}
		var batch []Stop
		for _, s := range r.stops {
			link := LineStop{LineID: r.lineID, StopCode: s.CP}
			if _, ok := seenLineStops[link]; !ok {
				seenLineStops[link] = struct{}{}
				allLineStops = append(allLineStops, link)
			}
			if _, ok := seenStops[s.CP]; !ok {
				seenStops[s.CP] = struct{}{}
				allStops = append(allStops, s)
				batch = append(batch, s)
			}
		}
		if onProgress != nil && len(batch) > 0 {
			onProgress(batch)
		}
	}

	return StopsFetchResult{
		Stops:      allStops,
		LineStops:  allLineStops,
		Total:      len(allStops),
		TotalLinks: len(allLineStops),
		TotalLines: len(allLines),
		Errors:     errCount,
		Elapsed:    time.Since(start),
	}, nil
}
