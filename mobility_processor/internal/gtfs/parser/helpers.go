package parser

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Raw map[string]string

func (r Raw) Required(k string) (string, error) {
	v := r[k]
	if v == "" {
		return "", fmt.Errorf("required field %s", k)
	}
	return v, nil
}
func (r Raw) Integer(k string, optional bool) (*int, error) {
	s := r[k]
	if s == "" && optional {
		return nil, nil
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid integer %s", k)
	}
	n := int(v)
	return &n, nil
}
func (r Raw) Number(k string, optional bool) (*float64, error) {
	s := r[k]
	if s == "" && optional {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("invalid finite number %s", k)
	}
	return &v, nil
}
func (r Raw) Enum(k string, optional bool, allowed ...int) (*int, error) {
	v, err := r.Integer(k, optional)
	if err != nil || v == nil {
		return v, err
	}
	for _, a := range allowed {
		if *v == a {
			return v, nil
		}
	}
	return nil, fmt.Errorf("invalid enum %s", k)
}
func ServiceSeconds(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 || len(parts[1]) != 2 || len(parts[2]) != 2 || len(parts[0]) < 1 {
		return nil, errors.New("invalid service time")
	}
	for _, p := range parts {
		for _, c := range p {
			if c < '0' || c > '9' {
				return nil, errors.New("invalid service time")
			}
		}
	}
	h, e := strconv.ParseInt(parts[0], 10, 32)
	m, e2 := strconv.Atoi(parts[1])
	sec, e3 := strconv.Atoi(parts[2])
	if e != nil || e2 != nil || e3 != nil || m > 59 || sec > 59 || h*3600+int64(m*60+sec) > math.MaxInt32 {
		return nil, errors.New("service time outside supported range")
	}
	v := int(h)*3600 + m*60 + sec
	return &v, nil
}
func read(path string, required []string, maxRows int, onRow func(Raw) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("%s header: %w", filepath.Base(path), err)
	}
	seen := map[string]bool{}
	for i, k := range header {
		if i == 0 {
			k = strings.TrimPrefix(k, "\ufeff")
			header[i] = k
		}
		if k == "" || seen[k] {
			return fmt.Errorf("%s duplicate/empty header", filepath.Base(path))
		}
		seen[k] = true
	}
	for _, k := range required {
		if !seen[k] {
			return fmt.Errorf("%s missing column %s", filepath.Base(path), k)
		}
	}
	count := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s CSV row %d: %w", filepath.Base(path), count+2, err)
		}
		count++
		if maxRows > 0 && count > maxRows {
			return fmt.Errorf("%s row limit exceeded", filepath.Base(path))
		}
		raw := Raw{}
		for i, v := range row {
			if !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
				return fmt.Errorf("%s row %d invalid UTF-8", filepath.Base(path), count+1)
			}
			raw[header[i]] = v
		}
		if err := onRow(raw); err != nil {
			return fmt.Errorf("%s row %d: %w", filepath.Base(path), count+1, err)
		}
	}
}
