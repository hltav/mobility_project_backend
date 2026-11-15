package parser

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type sortedRow struct {
	Key string `json:"key"`
	Row Raw    `json:"row"`
}

func rowKey(file string, r Raw) (string, error) {
	var id string
	var number int
	switch file {
	case "shapes.txt":
		id = r["shape_id"]
		v, e := r.Integer("shape_pt_sequence", false)
		if e != nil {
			return "", e
		}
		number = *v
	case "stop_times.txt":
		id = r["trip_id"]
		v, e := r.Integer("stop_sequence", false)
		if e != nil {
			return "", e
		}
		number = *v
	case "frequencies.txt":
		id = r["trip_id"]
		v, e := ServiceSeconds(r["start_time"])
		if e != nil || v == nil {
			return "", fmt.Errorf("invalid frequency time")
		}
		number = *v
	default:
		return "", fmt.Errorf("unsupported sorted entity")
	}
	if id == "" || number < 0 {
		return "", fmt.Errorf("invalid source key/sequence")
	}
	return fmt.Sprintf("%s\x00%010d", id, number), nil
}

// externalSort limits in-memory rows and merge fan-in independently of the file's size.
func externalSort(ctx context.Context, path, file, dir string, chunk int, count func(Raw) error) (string, error) {
	runs := []string{}
	rows := make([]sortedRow, 0, chunk)
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
		f, e := os.CreateTemp(dir, "run-*.jsonl")
		if e != nil {
			return e
		}
		w := bufio.NewWriterSize(f, 128*1024)
		enc := json.NewEncoder(w)
		for _, r := range rows {
			if e = enc.Encode(r); e != nil {
				break
			}
		}
		if e == nil {
			e = w.Flush()
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		runs = append(runs, f.Name())
		clear(rows)
		rows = rows[:0]
		return nil
	}
	err := read(path, headers[file], 0, func(r Raw) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := count(r); e != nil {
			return e
		}
		key, e := rowKey(file, r)
		if e != nil {
			return e
		}
		rows = append(rows, sortedRow{key, r})
		if len(rows) == chunk {
			return flush()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if err = flush(); err != nil {
		return "", err
	}
	if len(runs) == 0 {
		f, e := os.CreateTemp(dir, "empty-*.jsonl")
		if e != nil {
			return "", e
		}
		return f.Name(), f.Close()
	}
	for len(runs) > 1 {
		next := []string{}
		for start := 0; start < len(runs); start += 32 {
			end := min(start+32, len(runs))
			if end-start == 1 {
				next = append(next, runs[start])
				continue
			}
			target, e := mergeRuns(ctx, dir, runs[start:end])
			if e != nil {
				return "", e
			}
			next = append(next, target)
			for _, name := range runs[start:end] {
				if e = os.Remove(name); e != nil {
					return "", e
				}
			}
		}
		runs = next
	}
	target := filepath.Join(dir, file+".sorted.jsonl")
	if err = os.Rename(runs[0], target); err != nil {
		return "", err
	}
	return target, nil
}

type mergeHead struct {
	value   sortedRow
	decoder *json.Decoder
	file    *os.File
	order   int
}
type rowHeap []*mergeHead

func (h rowHeap) Len() int { return len(h) }
func (h rowHeap) Less(i, j int) bool {
	if h[i].value.Key == h[j].value.Key {
		return h[i].order < h[j].order
	}
	return h[i].value.Key < h[j].value.Key
}
func (h rowHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *rowHeap) Push(v any)   { *h = append(*h, v.(*mergeHead)) }
func (h *rowHeap) Pop() any     { a := *h; v := a[len(a)-1]; *h = a[:len(a)-1]; return v }
func mergeRuns(ctx context.Context, dir string, files []string) (string, error) {
	heads := rowHeap{}
	opened := []*os.File{}
	defer func() {
		for _, f := range opened {
			f.Close()
		}
	}()
	for i, name := range files {
		f, e := os.Open(name)
		if e != nil {
			return "", e
		}
		opened = append(opened, f)
		head := &mergeHead{decoder: json.NewDecoder(bufio.NewReaderSize(f, 32*1024)), file: f, order: i}
		if e = head.decoder.Decode(&head.value); e == nil {
			heap.Push(&heads, head)
		} else if e != io.EOF {
			return "", e
		}
	}
	out, e := os.CreateTemp(dir, "merge-*.jsonl")
	if e != nil {
		return "", e
	}
	defer out.Close()
	writer := bufio.NewWriterSize(out, 128*1024)
	encoder := json.NewEncoder(writer)
	for heads.Len() > 0 {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		head := heap.Pop(&heads).(*mergeHead)
		if e = encoder.Encode(head.value); e != nil {
			return "", e
		}
		e = head.decoder.Decode(&head.value)
		if e == nil {
			heap.Push(&heads, head)
		} else if e != io.EOF {
			return "", e
		}
	}
	if e = writer.Flush(); e != nil {
		return "", e
	}
	return out.Name(), nil
}
func walkSorted(ctx context.Context, path string, fn func(Raw) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 128*1024))
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		var r sortedRow
		e = dec.Decode(&r)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		if e = fn(r.Row); e != nil {
			return e
		}
	}
}
