package geocoding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	baseURL   = "https://nominatim.openstreetmap.org"
	userAgent = "MobilityApp/1.0 (mobility-project; hltav.dev@gmail.com)"
)

var (
	httpClient      = &http.Client{Timeout: 5 * time.Second}
	rateLimitMu     sync.Mutex
	lastNominatimAt time.Time
)

// Coords representa latitude e longitude.
type Coords struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Geocode converte um endereço em texto para coordenadas.
// Retorna nil se não encontrar resultado.
func Geocode(address string) (*Coords, error) {
	for _, candidate := range geocodeCandidates(address) {
		coords, err := geocodeOnce(candidate)
		if err != nil {
			return nil, err
		}
		if coords != nil {
			return coords, nil
		}
	}
	return nil, nil
}

func geocodeOnce(query string) (*Coords, error) {
	req, err := http.NewRequest("GET", baseURL+"/search", nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	q := req.URL.Query()
	q.Set("q", query+", São Paulo, Brasil")
	q.Set("format", "json")
	q.Set("limit", "1")
	q.Set("countrycodes", "br")
	q.Set("addressdetails", "0")
	req.URL.RawQuery = q.Encode()

	waitNominatimTurn()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocode request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geocode status %d", resp.StatusCode)
	}

	var results []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("geocode decode: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	var lat, lng float64
	if _, err := fmt.Sscanf(results[0].Lat, "%f", &lat); err != nil {
		return nil, fmt.Errorf("parse lat: %w", err)
	}
	if _, err := fmt.Sscanf(results[0].Lon, "%f", &lng); err != nil {
		return nil, fmt.Errorf("parse lon: %w", err)
	}

	return &Coords{Lat: lat, Lng: lng}, nil
}

func waitNominatimTurn() {
	rateLimitMu.Lock()
	defer rateLimitMu.Unlock()

	if !lastNominatimAt.IsZero() {
		elapsed := time.Since(lastNominatimAt)
		if elapsed < time.Second {
			time.Sleep(time.Second - elapsed)
		}
	}
	lastNominatimAt = time.Now()
}

// Reverse converte coordenadas em endereço legível.
func Reverse(lat, lng float64) (string, error) {
	req, err := http.NewRequest("GET", baseURL+"/reverse", nil)
	if err != nil {
		return "", fmt.Errorf("build reverse request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	q := req.URL.Query()
	q.Set("lat", fmt.Sprintf("%f", lat))
	q.Set("lon", fmt.Sprintf("%f", lng))
	q.Set("format", "json")
	q.Set("addressdetails", "1")
	req.URL.RawQuery = q.Encode()

	waitNominatimTurn()
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("reverse request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("reverse status %d", resp.StatusCode)
	}

	var result struct {
		Address struct {
			Road          string `json:"road"`
			Suburb        string `json:"suburb"`
			Neighbourhood string `json:"neighbourhood"`
			City          string `json:"city"`
			Town          string `json:"town"`
			Municipality  string `json:"municipality"`
		} `json:"address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("reverse decode: %w", err)
	}

	a := result.Address
	suburb := a.Suburb
	if suburb == "" {
		suburb = a.Neighbourhood
	}
	city := a.City
	if city == "" {
		city = a.Town
	}
	if city == "" {
		city = a.Municipality
	}

	parts := []string{}
	for _, p := range []string{a.Road, suburb, city} {
		if p != "" {
			parts = append(parts, p)
		}
	}

	return strings.Join(parts, ", "), nil
}

var abbreviations = map[string]string{
	"JD.":    "Jardim",
	"JD":     "Jardim",
	"AV.":    "Avenida",
	"AV":     "Avenida",
	"PQ.":    "Parque",
	"PQ":     "Parque",
	"VL.":    "Vila",
	"VL":     "Vila",
	"V.":     "Vila",
	"CJ.":    "Conjunto",
	"CJ":     "Conjunto",
	"R.":     "Rua",
	"ROD.":   "Rodovia",
	"EST.":   "Estrada",
	"EST":    "Estrada",
	"PCA.":   "Praça",
	"PÇA.":   "Praça",
	"PÇA":    "Praça",
	"TERM.":  "Terminal",
	"TERM":   "Terminal",
	"PRINC.": "Princesa",
	"PRINC":  "Princesa",
	"ESTR.":  "Estrada",
	"AL.":    "Alameda",
	"TRAV.":  "Travessa",
	"CHÁC.":  "Chácara",
	"CHAC.":  "Chácara",
	"CHÁC":   "Chácara",
	"CID.":   "Cidade",
	"CID":    "Cidade",
	"SHOP.":  "Shopping",
	"SHOP":   "Shopping",
	"CONJ.":  "Conjunto",
	"CONJ":   "Conjunto",
	"FAZ.":   "Fazenda",
	"FAZ":    "Fazenda",
	"RES.":   "Residencial",
	"RES":    "Residencial",
	"BALN.":  "Balneário",
	"CEM.":   "Cemitério",
	"COL.":   "Colônia",
	"DIV.":   "Divisa",
	"E.T.":   "Estação de Transferência",
	"HOSP.":  "Hospital",
	"LGO.":   "Largo",
	"STA.":   "Santa",
	"STO.":   "Santo",
	"SÍTIO":  "Sítio",
	"CEU":    "CEU",
	"CPTM":   "CPTM",
	"METRÔ":  "Metrô",
	"SESC":   "SESC",
}

var sectorSuffixPattern = regexp.MustCompile(`\s+[A-Z]?\d+[A-Z]?$`)

func geocodeCandidates(address string) []string {
	address = normalizeSpacing(address)
	if address == "" {
		return nil
	}

	candidates := make([]string, 0, 8)
	seen := make(map[string]struct{})
	add := func(value string) {
		value = normalizeAddress(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		candidates = append(candidates, value)
	}

	add(address)

	clean := strings.TrimSpace(strings.ReplaceAll(address, "/", " "))
	clean = strings.TrimSpace(strings.TrimSuffix(clean, " - CIRCULAR"))
	clean = strings.TrimSpace(strings.TrimPrefix(clean, "CIRCULAR "))
	add(clean)

	withoutSector := sectorSuffixPattern.ReplaceAllString(clean, "")
	add(withoutSector)

	if rest, ok := trimAnyPrefix(clean, "CONEXÃO "); ok {
		add(rest)
	}
	if rest, ok := trimAnyPrefix(clean, "CPTM "); ok {
		add("Estação " + rest)
		add("Estação CPTM " + rest)
	}
	if rest, ok := trimAnyPrefix(clean, "METRÔ "); ok {
		add("Estação " + rest)
		add("Estação de Metrô " + rest)
	}
	if rest, ok := trimAnyPrefix(clean, "TERM. EST. ", "TERM EST ", "TERM. ", "TERM "); ok {
		add("Terminal " + rest)
		add("Terminal " + stripStationPrefix(rest))
	}
	if rest, ok := trimAnyPrefix(clean, "EST. ", "EST "); ok {
		add("Estação " + rest)
		add("Estação " + strings.ReplaceAll(rest, "/", " "))
	}
	if rest, ok := trimAnyPrefix(clean, "SHOP. ", "SHOP "); ok {
		add("Shopping " + rest)
	}
	if rest, ok := trimAnyPrefix(clean, "LAR ESC. ", "LAR ESC "); ok {
		add("Lar Escola " + rest)
	}

	return candidates
}

func trimAnyPrefix(value string, prefixes ...string) (string, bool) {
	upper := strings.ToUpper(value)
	for _, prefix := range prefixes {
		if strings.HasPrefix(upper, prefix) {
			return strings.TrimSpace(value[len(prefix):]), true
		}
	}
	return "", false
}

func stripStationPrefix(value string) string {
	if rest, ok := trimAnyPrefix(value, "EST. ", "EST "); ok {
		return rest
	}
	return value
}

func normalizeSpacing(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "SHOP.", "SHOP. ")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}

	rawWords := strings.Fields(strings.ToUpper(address))
	normalized := make([]string, 0, len(rawWords))

	for _, word := range rawWords {
		if expanded, ok := abbreviations[word]; ok {
			normalized = append(normalized, expanded)
			continue
		}
		normalized = append(normalized, toTitleCase(strings.ToLower(word)))
	}

	return strings.Join(normalized, " ")
}

func toTitleCase(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
