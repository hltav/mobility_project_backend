package sptrans

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestStopsContextUsesOnlyRequestedCLAndPreservesCP(t *testing.T) {
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v2.1/Parada/BuscarParadasPorLinha" || r.URL.Query().Get("codigoLinha") != "101" {
			t.Fatal("unexpected scan", r.URL.Path)
		}
		return syntheticHTTP(200, `[{"cp":123,"np":"Synthetic","py":-23.55,"px":-46.63}]`), nil
	}, true)
	stops, err := c.GetStopsByLineContext(context.Background(), 101)
	if err != nil || len(stops) != 1 || stops[0].CP != 123 {
		t.Fatal(stops, err)
	}
}
func TestStopsContextTimeoutAndIncompleteBody(t *testing.T) {
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := c.GetStopsByLineContext(ctx, 101); err == nil {
		t.Fatal("timeout ignored")
	}
	c = syntheticClient(t, func(*http.Request) (*http.Response, error) { return syntheticHTTP(200, "null"), nil }, true)
	if _, err := c.GetStopsByLineContext(context.Background(), 101); err == nil {
		t.Fatal("null was treated as an empty official response")
	}
}
