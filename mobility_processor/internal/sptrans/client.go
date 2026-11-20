package sptrans

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	baseURL    = "http://api.olhovivo.sptrans.com.br/v2.1"
	sessionTTL = 1 * time.Hour
)

// Client é o cliente autenticado da API OlhoVivo/SPTrans.
// Mantém sessão via cookie jar e renova automaticamente quando expirada.
type Client struct {
	token      string
	httpClient *http.Client
	jar        *cookiejar.Jar
	authedAt   time.Time
	authGate   chan struct{}
}

func NewClient(token string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &Client{
		token:    token,
		authGate: make(chan struct{}, 1),
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		},
		jar: jar,
	}, nil
}

// Authenticate preserves the legacy API and shares the contextual authentication path.
func (c *Client) Authenticate() error {
	return c.authenticateContext(context.Background(), true)
}

func (c *Client) ensureAuthenticated() error {
	return c.authenticateContext(context.Background(), false)
}

// Serializes session renewal while allowing cancellation when waiting for it.
func (c *Client) authenticateContext(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return apiFailure(ErrAuthentication, 0, err)
	}
	select {
	case c.authGate <- struct{}{}:
		defer func() { <-c.authGate }()
	case <-ctx.Done():
		return apiFailure(ErrAuthentication, 0, ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return apiFailure(ErrAuthentication, 0, err)
	}
	if !force && !c.authedAt.IsZero() && time.Since(c.authedAt) <= sessionTTL {
		return nil
	}
	endpoint := baseURL + "/Login/Autenticar?" + url.Values{"token": {c.token}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(""))
	if err != nil {
		return apiFailure(ErrAuthentication, 0, nil)
	}
	body, err := c.responseBody(ctx, req, ErrAuthentication)
	if err != nil {
		return err
	}
	var ok bool
	if err := json.Unmarshal(body, &ok); err != nil || !ok {
		return apiFailure(ErrAuthentication, 0, nil)
	}
	c.authedAt = time.Now()
	return nil
}

func (c *Client) get(endpoint string, query url.Values) ([]byte, error) {
	if err := c.ensureAuthenticated(); err != nil {
		return nil, err
	}

	fullURL := baseURL + endpoint
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	resp, err := c.httpClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	var buf []byte
	buf = make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

// BusPosition representa a posição de um ônibus.
type BusPosition struct {
	Prefix     string  `json:"prefix"`
	Accessible bool    `json:"accessible"`
	CapturedAt string  `json:"captured_at"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
}

// GetBusPositionsByLine retorna as posições dos ônibus de uma linha.
func (c *Client) GetBusPositionsByLine(sptransLineID int) ([]BusPosition, error) {
	q := url.Values{"codigoLinha": {fmt.Sprintf("%d", sptransLineID)}}
	body, err := c.get("/Posicao/Linha", q)
	if err != nil {
		return nil, err
	}

	var data struct {
		Vehicles []struct {
			P  string  `json:"p"`
			A  bool    `json:"a"`
			Ta string  `json:"ta"`
			Py float64 `json:"py"`
			Px float64 `json:"px"`
		} `json:"vs"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode posições: %w", err)
	}

	result := make([]BusPosition, 0, len(data.Vehicles))
	for _, v := range data.Vehicles {
		result = append(result, BusPosition{
			Prefix:     v.P,
			Accessible: v.A,
			CapturedAt: v.Ta,
			Latitude:   v.Py,
			Longitude:  v.Px,
		})
	}
	return result, nil
}

// RawLine representa uma linha retornada pela API da SPTrans.
type RawLine struct {
	CL int    `json:"cl"` // sptrans_id
	LC bool   `json:"lc"` // linha circular
	LT string `json:"lt"` // número da linha (ex: "1016")
	TL int    `json:"tl"` // sufixo do letreiro (ex: 10, 21, 23...)
	SL int    `json:"sl"` // sentido (1 ou 2)
	TP string `json:"tp"` // letreiro destino principal
	TS string `json:"ts"` // letreiro destino secundário
}

// SearchLines busca linhas pelo termo informado.
func (c *Client) SearchLines(term string) ([]RawLine, error) {
	return c.SearchLinesContext(context.Background(), term)
}

func (c *Client) SearchLinesContext(ctx context.Context, term string) ([]RawLine, error) {
	if strings.TrimSpace(term) == "" {
		return nil, nil
	}

	q := url.Values{"termosBusca": {term}}
	if err := c.authenticateContext(ctx, false); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/Linha/Buscar?"+q.Encode(), nil)
	if err != nil {
		return nil, apiFailure(ErrTransport, 0, nil)
	}
	body, err := c.responseBody(ctx, req, ErrTransport)
	if err != nil {
		return nil, err
	}

	var lines []RawLine
	if err := json.Unmarshal(body, &lines); err != nil {
		return nil, fmt.Errorf("decode linhas: %w", err)
	}
	if lines == nil {
		return nil, apiFailure(ErrIncompleteResponse, 0, nil)
	}
	return lines, nil
}

// SyncResult resume o resultado de uma sincronização.
type SyncResult struct {
	Total    int
	Success  int
	Errors   []error
	Received int
}

// SyncLines sincroniza uma lista de termos em paralelo.
// workers controla a concorrência (ex: 10).
func (c *Client) SyncLines(terms []string, workers int, onLine func(RawLine) error) SyncResult {
	return c.SyncLinesContext(context.Background(), terms, workers, onLine)
}

// SyncLinesContext is the existing synchronization path with bounded cancellation.
func (c *Client) SyncLinesContext(ctx context.Context, terms []string, workers int, onLine func(RawLine) error) SyncResult {
	if workers < 1 || workers > 10 || onLine == nil {
		return SyncResult{Errors: []error{fmt.Errorf("invalid synchronization configuration")}}
	}
	termsCh := make(chan string, len(terms))
	for _, t := range terms {
		termsCh <- t
	}
	close(termsCh)

	type outcome struct {
		count    int
		received int
		err      error
	}
	resultsCh := make(chan outcome, len(terms))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for term := range termsCh {
				if ctx.Err() != nil {
					return
				}
				lines, err := c.SearchLinesContext(ctx, term)
				if err != nil {
					resultsCh <- outcome{err: fmt.Errorf("termo %q: %w", term, err)}
					continue
				}
				if len(lines) > 0 {
					resultsCh <- outcome{received: len(lines)}
				}
				for _, l := range lines {
					if ctx.Err() != nil {
						return
					}
					if err := onLine(l); err != nil {
						resultsCh <- outcome{err: err}
					} else {
						resultsCh <- outcome{count: 1}
					}
				}
			}
		}()
	}

	// Fecha o canal de resultados quando todas as goroutines terminarem
	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	var summary SyncResult
	for r := range resultsCh {
		if r.received > 0 {
			summary.Received += r.received
			continue
		}
		summary.Total++
		if r.err != nil {
			summary.Errors = append(summary.Errors, r.err)
		} else {
			summary.Success += r.count
		}
	}
	if ctx.Err() != nil {
		summary.Errors = append(summary.Errors, ctx.Err())
	}
	return summary
}

// UnmarshalJSON rejects missing source identity/direction fields instead of inventing TL/SL/LC defaults.
func (l *RawLine) UnmarshalJSON(data []byte) error {
	var v struct {
		CL *int   `json:"cl"`
		LC *bool  `json:"lc"`
		LT string `json:"lt"`
		TL *int   `json:"tl"`
		SL *int   `json:"sl"`
		TP string `json:"tp"`
		TS string `json:"ts"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	if v.CL == nil || *v.CL <= 0 || v.LC == nil || strings.TrimSpace(v.LT) == "" || v.TL == nil || *v.TL < 0 || v.SL == nil || (*v.SL != 1 && *v.SL != 2) {
		return fmt.Errorf("invalid SPTrans line: CL/LT/TL/SL/LC required")
	}
	*l = RawLine{CL: *v.CL, LC: *v.LC, LT: v.LT, TL: *v.TL, SL: *v.SL, TP: v.TP, TS: v.TS}
	return nil
}
