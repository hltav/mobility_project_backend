package sptrans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrInvalidLine        = errors.New("sptrans: invalid operational line")
	ErrAuthentication     = errors.New("sptrans: authentication failed")
	ErrHTTPStatus         = errors.New("sptrans: unexpected HTTP status")
	ErrTransport          = errors.New("sptrans: transport failed")
	ErrInvalidResponse    = errors.New("sptrans: invalid prediction response")
	ErrIncompleteResponse = errors.New("sptrans: incomplete response")
	ErrResponseTooLarge   = errors.New("sptrans: response exceeds size limit")
)

// APIError never includes URLs, tokens, cookies, response bodies or network error text.
type APIError struct {
	Kind       error
	StatusCode int
	cause      error
}

func (e *APIError) Error() string        { return fmt.Sprintf("%s (HTTP %d)", e.Kind, e.StatusCode) }
func (e *APIError) Is(target error) bool { return target == e.Kind }
func (e *APIError) Unwrap() error        { return e.cause }
func apiFailure(kind error, status int, cause error) error {
	return &APIError{Kind: kind, StatusCode: status, cause: cause}
}

type LinePredictions struct {
	// Request identity, not a claim that the provider includes CL in its payload.
	SptransLineID    int              `json:"sptrans_line_id"`
	ReferenceTimeRaw string           `json:"hr"`
	Stops            []StopPrediction `json:"ps"`
}
type StopPrediction struct {
	StopCode  int64               `json:"cp"`
	Name      *string             `json:"np,omitempty"`
	Latitude  *float64            `json:"py,omitempty"`
	Longitude *float64            `json:"px,omitempty"`
	Vehicles  []VehiclePrediction `json:"vs"`
}
type VehiclePrediction struct {
	Prefix         string   `json:"p"`
	ArrivalTimeRaw string   `json:"t"`
	CapturedAtRaw  *string  `json:"ta,omitempty"`
	Accessible     *bool    `json:"a,omitempty"`
	Latitude       *float64 `json:"py,omitempty"`
	Longitude      *float64 `json:"px,omitempty"`
}

// GetLinePredictions reuses the existing HTTP client, session and cookie jar.
// hr/t/ta remain raw source strings: no dates, ETA or GTFS identity are inferred.
func (c *Client) GetLinePredictions(ctx context.Context, sptransLineID int) (*LinePredictions, error) {
	if sptransLineID <= 0 {
		return nil, apiFailure(ErrInvalidLine, 0, nil)
	}
	if err := ctx.Err(); err != nil {
		return nil, apiFailure(ErrTransport, 0, err)
	}
	if err := c.authenticateContext(ctx, false); err != nil {
		return nil, err
	}
	endpoint := baseURL + "/Previsao/Linha?" + url.Values{"codigoLinha": {strconv.Itoa(sptransLineID)}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, apiFailure(ErrTransport, 0, nil)
	}
	body, err := c.responseBody(ctx, req, ErrTransport)
	if err != nil {
		return nil, err
	}
	return decodeLinePredictions(body, sptransLineID)
}

// responseBody is also used by session renewal; deadlines apply to both requests.
func (c *Client) responseBody(ctx context.Context, req *http.Request, transportKind error) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		cause := ctx.Err()
		var timeout net.Error
		if cause == nil && errors.As(err, &timeout) && timeout.Timeout() {
			cause = context.DeadlineExceeded
		}
		return nil, apiFailure(transportKind, 0, cause)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, apiFailure(ErrAuthentication, resp.StatusCode, nil)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiFailure(ErrHTTPStatus, resp.StatusCode, nil)
	}
	const maxBody = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if ctx.Err() != nil {
		return nil, apiFailure(transportKind, resp.StatusCode, ctx.Err())
	}
	if err != nil {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return nil, apiFailure(transportKind, resp.StatusCode, context.DeadlineExceeded)
		}
		return nil, apiFailure(ErrIncompleteResponse, resp.StatusCode, nil)
	}
	if len(body) > maxBody {
		return nil, apiFailure(ErrResponseTooLarge, resp.StatusCode, nil)
	}
	return body, nil
}

func decodeLinePredictions(body []byte, cl int) (*LinePredictions, error) {
	var wire struct {
		Reference *string `json:"hr"`
		CL        *int    `json:"cl"`
		Stops     *[]struct {
			CP        *int64   `json:"cp"`
			Name      *string  `json:"np"`
			Latitude  *float64 `json:"py"`
			Longitude *float64 `json:"px"`
			Vehicles  *[]struct {
				Prefix     json.RawMessage `json:"p"`
				Arrival    *string         `json:"t"`
				Captured   *string         `json:"ta"`
				Accessible *bool           `json:"a"`
				Latitude   *float64        `json:"py"`
				Longitude  *float64        `json:"px"`
			} `json:"vs"`
		} `json:"ps"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		if !json.Valid(body) && (len(bytes.TrimSpace(body)) == 0 || unexpectedJSONEnd(body)) {
			return nil, apiFailure(ErrIncompleteResponse, 0, nil)
		}
		return nil, apiFailure(ErrInvalidResponse, 0, nil)
	}
	if wire.Reference == nil || wire.Stops == nil {
		return nil, apiFailure(ErrIncompleteResponse, 0, nil)
	}
	if strings.TrimSpace(*wire.Reference) == "" {
		return nil, apiFailure(ErrInvalidResponse, 0, nil)
	}
	if wire.CL != nil && *wire.CL != cl {
		return nil, apiFailure(ErrInvalidResponse, 0, nil)
	}
	result := &LinePredictions{SptransLineID: cl, ReferenceTimeRaw: *wire.Reference, Stops: make([]StopPrediction, 0, len(*wire.Stops))}
	for _, s := range *wire.Stops {
		if s.CP == nil || s.Vehicles == nil {
			return nil, apiFailure(ErrIncompleteResponse, 0, nil)
		}
		if *s.CP <= 0 {
			return nil, apiFailure(ErrInvalidResponse, 0, nil)
		}
		stop := StopPrediction{StopCode: *s.CP, Name: s.Name, Latitude: s.Latitude, Longitude: s.Longitude, Vehicles: make([]VehiclePrediction, 0, len(*s.Vehicles))}
		for _, v := range *s.Vehicles {
			prefix, err := predictionPrefix(v.Prefix)
			if err != nil {
				return nil, err
			}
			if v.Arrival == nil {
				return nil, apiFailure(ErrIncompleteResponse, 0, nil)
			}
			if strings.TrimSpace(*v.Arrival) == "" {
				return nil, apiFailure(ErrInvalidResponse, 0, nil)
			}
			stop.Vehicles = append(stop.Vehicles, VehiclePrediction{Prefix: prefix, ArrivalTimeRaw: *v.Arrival, CapturedAtRaw: v.Captured,
				Accessible: v.Accessible, Latitude: v.Latitude, Longitude: v.Longitude})
		}
		result.Stops = append(result.Stops, stop)
	}
	return result, nil
}

func unexpectedJSONEnd(body []byte) bool {
	var raw json.RawMessage
	err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw)
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func predictionPrefix(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", apiFailure(ErrIncompleteResponse, 0, nil)
	}
	var s string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil || strings.TrimSpace(s) == "" {
			return "", apiFailure(ErrInvalidResponse, 0, nil)
		}
		return s, nil
	}
	// Numeric identifiers are converted lexically, never via float or an integer cast.
	s = string(raw)
	for _, r := range s {
		if r < '0' || r > '9' {
			return "", apiFailure(ErrInvalidResponse, 0, nil)
		}
	}
	return s, nil
}
