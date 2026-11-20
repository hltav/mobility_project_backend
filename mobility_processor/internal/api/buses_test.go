package api

import (
	"io"
	"mobility_processor/internal/sptrans"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type busTransportFunc func(*http.Request) (*http.Response, error)

func (f busTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type demandRecorderStub struct{ cls []int }

func (s *demandRecorderStub) RecordDemand(cl int) bool { s.cls = append(s.cls, cl); return false }

func TestBusPositionsDemandNeverQueriesPredictionsOrChangesGPSResponse(t *testing.T) {
	// Synthetic transport handles every request locally; no socket is opened.
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	requests := 0
	http.DefaultTransport = busTransportFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body := "true"
		if r.URL.Path == "/v2.1/Posicao/Linha" {
			body = `{"vs":[{"p":"00017","ta":"synthetic","py":-23.55,"px":-46.63}]}`
		} else if r.URL.Path != "/v2.1/Login/Autenticar" {
			t.Fatal("unexpected external operation", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	client, err := sptrans.NewClient("synthetic-test-token")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(client, nil, nil, nil)
	recorder := &demandRecorderStub{}
	h.SetPredictionDemandRecorder(recorder)
	r := httptest.NewRequest(http.MethodGet, "/buses/2506", nil)
	r.SetPathValue("sptrans_id", "2506")
	w := httptest.NewRecorder()
	h.BusPositions(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "00017") || requests != 2 || len(recorder.cls) != 1 || recorder.cls[0] != 2506 {
		t.Fatal("GPS response or demand signal changed", w.Code, requests, recorder.cls)
	}
}

func TestBusPositionsInvalidCLNeverRecordsDemand(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil)
	recorder := &demandRecorderStub{}
	h.SetPredictionDemandRecorder(recorder)
	r := httptest.NewRequest(http.MethodGet, "/buses/0", nil)
	r.SetPathValue("sptrans_id", "0")
	w := httptest.NewRecorder()
	h.BusPositions(w, r)
	if w.Code != http.StatusBadRequest || len(recorder.cls) != 0 {
		t.Fatal("invalid CL reached demand/GPS")
	}
}

func TestBusSnapshotPreservesProviderObservationAndCoordinates(t *testing.T) {
	positions := []sptrans.BusPosition{{Prefix: "fixture-01", CapturedAt: "2026-11-18T12:00:00Z", Latitude: -23.55, Longitude: -46.63}}
	snapshot := busSnapshot(positions)
	if len(snapshot) != 1 || snapshot[0].CapturedAt != positions[0].CapturedAt || snapshot[0].Prefix != "fixture-01" ||
		snapshot[0].Lat != -23.55 || snapshot[0].Lng != -46.63 {
		t.Fatal(snapshot)
	}
	if snapshot := busSnapshot(nil); snapshot == nil || len(snapshot) != 0 {
		t.Fatal("empty observation must remain an empty snapshot")
	}
	positions[0].CapturedAt = ""
	if busSnapshot(positions)[0].CapturedAt != "" {
		t.Fatal("invented observation time")
	}
}
