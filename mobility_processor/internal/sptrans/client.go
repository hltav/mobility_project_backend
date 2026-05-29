package sptrans

import (
	"encoding/json"
	"fmt"
	"log"
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
	mu         sync.Mutex
}

func NewClient(token string) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	return &Client{
		token: token,
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 10 * time.Second,
		},
		jar: jar,
	}, nil
}

// Authenticate autentica na API e armazena o cookie de sessão.
func (c *Client) Authenticate() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	endpoint := fmt.Sprintf("%s/Login/Autenticar?token=%s", baseURL, c.token)
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(""))
	if err != nil {
		return fmt.Errorf("criar request de auth: %w", err)
	}
	req.Header.Set("Content-Length", "0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request de auth: %w", err)
	}
	defer resp.Body.Close()

	var ok bool
	if err := json.NewDecoder(resp.Body).Decode(&ok); err != nil || !ok {
		return fmt.Errorf("auth retornou false ou erro de decode")
	}

	c.authedAt = time.Now()
	log.Println("[SPTrans] Autenticado com sucesso")
	return nil
}

// ensureAuthenticated reautentica se a sessão tiver expirado.
func (c *Client) ensureAuthenticated() error {
	c.mu.Lock()
	expired := c.authedAt.IsZero() || time.Since(c.authedAt) > sessionTTL
	c.mu.Unlock()

	if expired {
		return c.Authenticate()
	}
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
	LT string `json:"lt"` // número da linha
	TP string `json:"tp"` // letreiro destino principal
	TS string `json:"ts"` // letreiro destino secundário
	LC bool   `json:"lc"` // linha circular
}

// SearchLines busca linhas pelo termo informado.
func (c *Client) SearchLines(term string) ([]RawLine, error) {
	if strings.TrimSpace(term) == "" {
		return nil, nil
	}

	q := url.Values{"termosBusca": {term}}
	body, err := c.get("/Linha/Buscar", q)
	if err != nil {
		return nil, err
	}

	var lines []RawLine
	if err := json.Unmarshal(body, &lines); err != nil {
		return nil, fmt.Errorf("decode linhas: %w", err)
	}
	return lines, nil
}

// SyncResult resume o resultado de uma sincronização.
type SyncResult struct {
	Total   int
	Success int
	Errors  []error
}

// SyncLines sincroniza uma lista de termos em paralelo.
// workers controla a concorrência (ex: 10).
func (c *Client) SyncLines(terms []string, workers int, onLine func(RawLine) error) SyncResult {
	termsCh := make(chan string, len(terms))
	for _, t := range terms {
		termsCh <- t
	}
	close(termsCh)

	type outcome struct {
		count int
		err   error
	}
	resultsCh := make(chan outcome, len(terms))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for term := range termsCh {
				lines, err := c.SearchLines(term)
				if err != nil {
					resultsCh <- outcome{err: fmt.Errorf("termo %q: %w", term, err)}
					continue
				}
				for _, l := range lines {
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
		summary.Total++
		if r.err != nil {
			summary.Errors = append(summary.Errors, r.err)
		} else {
			summary.Success += r.count
		}
	}
	return summary
}
