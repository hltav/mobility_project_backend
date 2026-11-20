package sptrans

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// All payloads below are SYNTHETIC, not captured/homologated SPTrans samples.
// Contract assumptions: hr and ps are required; each stop requires numeric CP
// and vs (possibly []); each vehicle requires p and nonblank t. String prefixes
// retain zeros; integer JSON prefixes are accepted lexically. ta/coords/name/a
// are optional. No assumption assigns a calendar date to hr or t.
const syntheticPredictions = `{"hr":"23:58","ps":[
 {"cp":880001,"np":"Synthetic A","vs":[{"p":"00017","t":"00:03","ta":"2026-11-19T23:57:00-03:00"},{"p":123456789012345678901,"t":"00:05"}]},
 {"cp":880002,"vs":[{"p":"00017","t":"00:10"}]}]}`

type predictionRoundTripper func(*http.Request) (*http.Response, error)

func (f predictionRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func syntheticHTTP(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func syntheticClient(t *testing.T, transport predictionRoundTripper, authenticated bool) *Client {
	t.Helper()
	c, err := NewClient("synthetic-token&not-a-real-secret")
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Transport = transport
	if authenticated {
		c.authedAt = time.Now()
	}
	return c
}

func TestLinePredictionsSyntheticContractReusesSessionAndLegacyGPS(t *testing.T) {
	logins, predictions, gps := 0, 0, 0
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v2.1/Login/Autenticar" {
			logins++
			if r.Method != http.MethodPost || r.URL.Query().Get("token") != "synthetic-token&not-a-real-secret" {
				t.Fatal("invalid authentication request")
			}
			response := syntheticHTTP(200, "true")
			response.Header.Set("Set-Cookie", "session=synthetic; Path=/")
			return response, nil
		}
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "synthetic" {
			t.Fatal("session cookie was not reused")
		}
		switch r.URL.Path {
		case "/v2.1/Previsao/Linha":
			predictions++
			if r.Method != http.MethodGet || r.URL.Query().Get("codigoLinha") != "2506" {
				t.Fatal("wrong operational identity")
			}
			return syntheticHTTP(200, syntheticPredictions), nil
		case "/v2.1/Posicao/Linha":
			gps++
			return syntheticHTTP(200, `{"vs":[]}`), nil
		default:
			t.Fatal("unexpected request")
			return nil, nil
		}
	}, false)
	for range 2 {
		result, err := c.GetLinePredictions(context.Background(), 2506)
		if err != nil {
			t.Fatal(err)
		}
		if result.SptransLineID != 2506 || result.ReferenceTimeRaw != "23:58" || len(result.Stops) != 2 || result.Stops[0].StopCode != 880001 {
			t.Fatal(result)
		}
		vehicles := result.Stops[0].Vehicles
		if vehicles[0].Prefix != "00017" || vehicles[1].Prefix != "123456789012345678901" || vehicles[0].ArrivalTimeRaw != "00:03" {
			t.Fatal(vehicles)
		}
		if vehicles[0].CapturedAtRaw == nil || *vehicles[0].CapturedAtRaw != "2026-11-19T23:57:00-03:00" || vehicles[1].CapturedAtRaw != nil {
			t.Fatal("invented or changed capture timestamp")
		}
	}
	if _, err := c.GetBusPositionsByLine(2506); err != nil {
		t.Fatal(err)
	}
	if logins != 1 || predictions != 2 || gps != 1 {
		t.Fatal(logins, predictions, gps)
	}
}

func TestLinePredictionEmptyAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"empty stops", `{"hr":"12:00","ps":[]}`, nil},
		{"empty vehicles", `{"hr":"12:00","ps":[{"cp":1,"vs":[]}]}`, nil},
		{"missing list", `{"hr":"12:00"}`, ErrIncompleteResponse},
		{"null list", `{"hr":"12:00","ps":null}`, ErrIncompleteResponse},
		{"missing vehicles", `{"hr":"12:00","ps":[{"cp":1}]}`, ErrIncompleteResponse},
		{"missing arrival", `{"hr":"12:00","ps":[{"cp":1,"vs":[{"p":"001"}]}]}`, ErrIncompleteResponse},
		{"missing prefix", `{"hr":"12:00","ps":[{"cp":1,"vs":[{"t":"12:03"}]}]}`, ErrIncompleteResponse},
		{"invalid json", `{"hr":!}`, ErrInvalidResponse},
		{"truncated json", `{"hr":"12:00","ps":[`, ErrIncompleteResponse},
		{"trailing document", `{"hr":"12:00","ps":[]} {}`, ErrInvalidResponse},
		{"missing body", ``, ErrIncompleteResponse},
		{"null body", `null`, ErrIncompleteResponse},
		{"wrong CL", `{"cl":99,"hr":"12:00","ps":[]}`, ErrInvalidResponse},
		{"invalid CP", `{"hr":"12:00","ps":[{"cp":0,"vs":[]}]}`, ErrInvalidResponse},
		{"fractional prefix", `{"hr":"12:00","ps":[{"cp":1,"vs":[{"p":1.5,"t":"12:03"}]}]}`, ErrInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := syntheticClient(t, func(*http.Request) (*http.Response, error) { return syntheticHTTP(200, tc.body), nil }, true)
			result, err := c.GetLinePredictions(context.Background(), 2506)
			if tc.want == nil {
				if err != nil || result == nil || result.Stops == nil {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, tc.want) || result != nil {
				t.Fatal(result, err)
			}
		})
	}
}

func TestLinePredictionHTTPAndAuthenticationErrors(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		c := syntheticClient(t, func(*http.Request) (*http.Response, error) { return syntheticHTTP(status, "sensitive-body"), nil }, true)
		_, err := c.GetLinePredictions(context.Background(), 2506)
		want := ErrHTTPStatus
		if status == 401 || status == 403 {
			want = ErrAuthentication
		}
		var api *APIError
		if !errors.Is(err, want) || !errors.As(err, &api) || api.StatusCode != status || strings.Contains(err.Error(), "sensitive-body") {
			t.Fatal(err)
		}
	}
	c := syntheticClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatal("prediction fetched without authenticated session")
		}
		return syntheticHTTP(200, "false"), nil
	}, false)
	if _, err := c.GetLinePredictions(context.Background(), 2506); !errors.Is(err, ErrAuthentication) {
		t.Fatal(err)
	}
}

type brokenPredictionBody struct{}

func (brokenPredictionBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenPredictionBody) Close() error             { return nil }

type timeoutPredictionBody struct{}

func (timeoutPredictionBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (timeoutPredictionBody) Close() error             { return nil }

func TestLinePredictionIncompleteBodySizeAndErrorRedaction(t *testing.T) {
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		response := syntheticHTTP(200, "")
		response.Body = brokenPredictionBody{}
		return response, nil
	}, true)
	if _, err := c.GetLinePredictions(context.Background(), 2506); !errors.Is(err, ErrIncompleteResponse) {
		t.Fatal(err)
	}
	c.httpClient.Transport = predictionRoundTripper(func(*http.Request) (*http.Response, error) {
		response := syntheticHTTP(200, "")
		response.Body = timeoutPredictionBody{}
		return response, nil
	})
	if _, err := c.GetLinePredictions(context.Background(), 2506); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	c.httpClient.Transport = predictionRoundTripper(func(*http.Request) (*http.Response, error) {
		return syntheticHTTP(200, strings.Repeat(" ", (2<<20)+1)), nil
	})
	if _, err := c.GetLinePredictions(context.Background(), 2506); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
	c.httpClient.Transport = predictionRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("synthetic-token&not-a-real-secret Cookie: synthetic")
	})
	_, err := c.GetLinePredictions(context.Background(), 2506)
	if !errors.Is(err, ErrTransport) || strings.Contains(err.Error(), "synthetic") || errors.Unwrap(err) != nil {
		t.Fatal("transport details were exposed")
	}
}

func TestLinePredictionCancellationAndTimeoutWithoutNetwork(t *testing.T) {
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("request after pre-cancellation")
		return nil, nil
	}, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetLinePredictions(ctx, 2506); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Wait behind another login: cancellation must also cover the session gate.
	c.authGate <- struct{}{}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	_, err := c.GetLinePredictions(ctx, 2506)
	cancel()
	<-c.authGate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	c.httpClient.Timeout = time.Millisecond
	c.httpClient.Transport = predictionRoundTripper(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	if _, err := c.GetLinePredictions(context.Background(), 2506); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// The same request context must cancel authentication, not only the prediction GET.
	c.authedAt = time.Time{}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	_, err = c.GetLinePredictions(ctx, 2506)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestInvalidPredictionLineDoesNotAuthenticate(t *testing.T) {
	c := syntheticClient(t, func(*http.Request) (*http.Response, error) { t.Fatal("invalid CL reached transport"); return nil, nil }, false)
	if _, err := c.GetLinePredictions(context.Background(), 0); !errors.Is(err, ErrInvalidLine) {
		t.Fatal(err)
	}
}
