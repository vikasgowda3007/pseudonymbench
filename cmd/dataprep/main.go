// Command dataprep downloads a public PII dataset and converts it to the JSONL
// format the eval tool reads. Nothing is stored in the repository: data is
// fetched on demand so its licence terms stay with its source.
//
//	dataprep nemotron -n 3000 -out data/nemotron-pii.jsonl
//
// nvidia/Nemotron-PII is CC-BY-4.0 (https://huggingface.co/datasets/nvidia/Nemotron-PII).
// Cite it when publishing numbers computed on it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/eval"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

const rowsAPI = "https://datasets-server.huggingface.co/rows"

func main() {
	if len(os.Args) < 2 || os.Args[1] != "nemotron" {
		fmt.Fprintln(os.Stderr, "usage: dataprep nemotron [-n 3000] [-split test] [-offset 0] [-out data/nemotron-pii.jsonl]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("nemotron", flag.ExitOnError)
	n := fs.Int("n", 3000, "rows to fetch")
	split := fs.String("split", "test", "dataset split: train or test")
	offset := fs.Int("offset", 0, "first row")
	out := fs.String("out", "data/nemotron-pii.jsonl", "output file")
	fs.Parse(os.Args[2:])
	if err := run(*n, *split, *offset, *out); err != nil {
		fmt.Fprintln(os.Stderr, "dataprep:", err)
		os.Exit(1)
	}
}

type rowsResp struct {
	Rows []struct {
		Row struct {
			Text  string `json:"text"`
			Spans string `json:"spans"`
		} `json:"row"`
	} `json:"rows"`
}

func run(n int, split string, offset int, out string) error {
	var samples []synth.Sample
	dropped, bad := 0, 0
	client := &http.Client{Timeout: 60 * time.Second}
	for got := 0; got < n; {
		length := min(100, n-got) // the API caps a page at 100 rows
		q := url.Values{"dataset": {"nvidia/Nemotron-PII"}, "config": {"default"}, "split": {split},
			"offset": {fmt.Sprint(offset + got)}, "length": {fmt.Sprint(length)}}
		page, err := fetch(client, rowsAPI+"?"+q.Encode())
		if err != nil {
			return err
		}
		if len(page.Rows) == 0 {
			break
		}
		for _, r := range page.Rows {
			s, d, err := eval.FromNemotron(r.Row.Text, r.Row.Spans)
			if err != nil {
				bad++
				continue
			}
			dropped += d
			samples = append(samples, s)
		}
		got += len(page.Rows)
		time.Sleep(400 * time.Millisecond) // be polite to the shared API
	}
	if len(samples) == 0 {
		return fmt.Errorf("no rows fetched")
	}
	if err := os.MkdirAll("data", 0o755); err != nil {
		return err
	}
	if err := eval.SaveJSONL(out, samples); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, s := range samples {
		for _, sp := range s.Spans {
			counts[string(sp.Kind)]++
		}
	}
	fmt.Printf("wrote %d samples to %s (unparseable rows %d, inconsistent spans dropped %d)\ngold spans by kind: %v\n",
		len(samples), out, bad, dropped, counts)
	return nil
}

func fetch(c *http.Client, u string) (rowsResp, error) {
	var out rowsResp
	for attempt := 1; ; attempt++ {
		resp, err := c.Get(u)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return out, json.NewDecoder(resp.Body).Decode(&out)
		}
		status := 0
		if resp != nil {
			status = resp.StatusCode
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if attempt >= 6 {
			return out, fmt.Errorf("fetch failed after %d attempts (status %d, err %v)", attempt, status, err)
		}
		time.Sleep(time.Duration(attempt) * 5 * time.Second) // the API rate-limits; back off
	}
}
