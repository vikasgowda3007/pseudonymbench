// Command eval measures what a masking proxy does to PII, from the outside.
//
//	eval gen      -n 2000 -seed 1 -out data/synthetic.jsonl
//	eval detector -data data/synthetic.jsonl
//	eval proxy    -data data/synthetic.jsonl -target URL -mock URL -label NAME
//	eval presidio -data data/synthetic.jsonl -url http://localhost:5002   (needs a Presidio analyzer)
//	eval fields   -target URL -mock URL -label NAME -out results/fields-NAME.json
//	eval merge    results/fields-*.json
//
// "proxy" and "fields" need the mock upstream (cmd/mockupstream) behind the
// proxy under test: the mock records what it received, and these commands read
// that back. Start the mock with -first-token 0 -token-delay 0 -chunks 1 so
// evaluation does not wait on simulated generation time.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/eval"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = cmdGen(os.Args[2:])
	case "detector":
		err = cmdDetector(os.Args[2:])
	case "proxy":
		err = cmdProxy(os.Args[2:])
	case "presidio":
		err = cmdPresidio(os.Args[2:])
	case "fields":
		err = cmdFields(os.Args[2:])
	case "merge":
		err = cmdMerge(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: eval gen|detector|proxy|presidio|fields|merge [flags]  (run a subcommand with -h)")
	os.Exit(2)
}

func cmdGen(args []string) error {
	fs := flag.NewFlagSet("gen", flag.ExitOnError)
	n := fs.Int("n", 2000, "samples")
	seed := fs.Int64("seed", 1, "seed")
	out := fs.String("out", "data/synthetic.jsonl", "output file")
	fs.Parse(args)
	if err := os.MkdirAll(dir(*out), 0o755); err != nil {
		return err
	}
	if err := eval.SaveJSONL(*out, synth.New(*seed).Dataset(*n)); err != nil {
		return err
	}
	fmt.Printf("wrote %d samples to %s (seed %d)\n", *n, *out, *seed)
	return nil
}

func cmdDetector(args []string) error {
	fs := flag.NewFlagSet("detector", flag.ExitOnError)
	data := fs.String("data", "data/synthetic.jsonl", "labelled JSONL")
	label := fs.String("label", "regex tier (detector only)", "name in the report")
	out := fs.String("out", "", "write the report JSON here")
	misses := fs.Int("misses", 0, "also print up to this many missed gold spans per kind (public or synthetic data only)")
	fs.Parse(args)
	samples, err := eval.LoadJSONL(*data)
	if err != nil {
		return err
	}
	preds := eval.DetectorRegions(samples)
	if err := emit(eval.Score(*label, samples, preds, nil), *out); err != nil {
		return err
	}
	if *misses > 0 {
		fmt.Print(eval.Diagnose(samples, preds, *misses))
	}
	return nil
}

func cmdProxy(args []string) error {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	data := fs.String("data", "data/synthetic.jsonl", "labelled JSONL")
	target := fs.String("target", "http://localhost:8080/v1/chat/completions", "chat-completions URL of the proxy under test")
	mock := fs.String("mock", "http://localhost:9000", "mock upstream base URL")
	label := fs.String("label", "pseudonymbench", "name in the report")
	conc := fs.Int("c", 16, "concurrent requests")
	seq := fs.Bool("sequential", false, "one request at a time, read back via the mock's /_last (for proxies that drop custom headers)")
	out := fs.String("out", "", "write the report JSON here")
	var hdr headers
	fs.Var(&hdr, "H", "extra request header 'Name: value' (repeatable), e.g. Authorization")
	fs.Parse(args)

	samples, err := eval.LoadJSONL(*data)
	if err != nil {
		return err
	}
	t := eval.Target{URL: *target, MockURL: *mock, Headers: hdr.m, Concurrency: *conc, Sequential: *seq}
	runID := fmt.Sprintf("p%d", time.Now().UnixNano())
	preds, errs := eval.RunProxy(context.Background(), t, runID, samples)
	return emit(eval.Score(*label, samples, preds, errs), *out)
}

func cmdPresidio(args []string) error {
	fs := flag.NewFlagSet("presidio", flag.ExitOnError)
	data := fs.String("data", "data/synthetic.jsonl", "labelled JSONL")
	url := fs.String("url", "http://localhost:5002", "Presidio analyzer base URL")
	score := fs.Float64("score", 0.4, "minimum score to count a result")
	label := fs.String("label", "Presidio analyzer", "name in the report")
	conc := fs.Int("c", 8, "concurrent requests")
	out := fs.String("out", "", "write the report JSON here")
	misses := fs.Int("misses", 0, "also print up to this many missed gold spans per kind (public or synthetic data only)")
	fs.Parse(args)
	samples, err := eval.LoadJSONL(*data)
	if err != nil {
		return err
	}
	preds, errs := eval.PresidioRegions(context.Background(), *url, *score, *conc, samples)
	if err := emit(eval.Score(*label, samples, preds, errs), *out); err != nil {
		return err
	}
	if *misses > 0 {
		fmt.Print(eval.Diagnose(samples, preds, *misses))
	}
	return nil
}

func cmdFields(args []string) error {
	fs := flag.NewFlagSet("fields", flag.ExitOnError)
	target := fs.String("target", "http://localhost:8080/v1/chat/completions", "chat-completions URL of the proxy under test")
	mock := fs.String("mock", "http://localhost:9000", "mock upstream base URL")
	label := fs.String("label", "pseudonymbench", "name in the report")
	seq := fs.Bool("sequential", false, "read back via the mock's /_last (for proxies that drop custom headers)")
	out := fs.String("out", "", "write the report JSON here")
	var hdr headers
	fs.Var(&hdr, "H", "extra request header 'Name: value' (repeatable)")
	fs.Parse(args)

	t := eval.Target{URL: *target, MockURL: *mock, Headers: hdr.m, Sequential: *seq}
	rep := eval.RunFields(context.Background(), t, fmt.Sprintf("f%d", time.Now().UnixNano()), *label)
	fmt.Print(eval.FieldsMarkdown([]eval.FieldReport{rep}))
	if *out != "" {
		return writeJSON(*out, rep)
	}
	return nil
}

func cmdMerge(args []string) error {
	var reports []eval.FieldReport
	for _, p := range args {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var r eval.FieldReport
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		reports = append(reports, r)
	}
	fmt.Print(eval.FieldsMarkdown(reports))
	return nil
}

func emit(rep eval.Report, out string) error {
	fmt.Println(rep.Markdown())
	if out != "" {
		return writeJSON(out, rep)
	}
	return nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func dir(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return "."
}

// headers is a repeatable -H flag.
type headers struct{ m map[string]string }

func (h *headers) String() string { return "" }

func (h *headers) Set(v string) error {
	name, val, ok := strings.Cut(v, ":")
	if !ok {
		return fmt.Errorf("header %q: want 'Name: value'", v)
	}
	if h.m == nil {
		h.m = map[string]string{}
	}
	h.m[strings.TrimSpace(name)] = strings.TrimSpace(val)
	return nil
}
