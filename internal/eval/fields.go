package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
)

// Outcome is what happened to a canary planted in one request field.
type Outcome string

const (
	Masked   Outcome = "masked"   // the canary did not reach the upstream
	Leaked   Outcome = "LEAKED"   // the canary reached the upstream unchanged
	Rejected Outcome = "rejected" // the proxy refused the request (fails closed)
	Dropped  Outcome = "dropped"  // the field never reached the upstream: removed, not masked
	Failed   Outcome = "error"    // the harness could not tell
)

// FieldCase plants one canary in one place of a chat-completions request.
type FieldCase struct {
	ID    string
	Field string
	Build func(canary string) map[string]any
}

type obj = map[string]any

func base(msgs ...any) obj {
	if len(msgs) == 0 {
		msgs = []any{obj{"role": "user", "content": "hello"}}
	}
	return obj{"model": "eval-model", "messages": msgs}
}

func with(b obj, k string, v any) obj { b[k] = v; return b }

func toolCallMsgs(args string) []any {
	return []any{
		obj{"role": "user", "content": "hello"},
		obj{"role": "assistant", "content": nil, "tool_calls": []any{obj{
			"id": "call_1", "type": "function", "function": obj{"name": "lookup", "arguments": args}}}},
		obj{"role": "tool", "tool_call_id": "call_1", "content": "ok"},
	}
}

func argsJSON(m obj) string { b, _ := json.Marshal(m); return string(b) }

// Cases lists every request field the matrix probes. Fields the API restricts
// so tightly that PII cannot legally appear (messages[].name allows only
// [a-zA-Z0-9_-]) are left out.
func Cases() []FieldCase {
	return []FieldCase{
		{"content-string", "messages[].content (string)", func(c string) obj {
			return base(obj{"role": "user", "content": "mail " + c})
		}},
		{"content-parts", "messages[].content[].text", func(c string) obj {
			return base(obj{"role": "user", "content": []any{obj{"type": "text", "text": "mail " + c}}})
		}},
		{"system", "system message", func(c string) obj {
			return base(obj{"role": "system", "content": "owner " + c}, obj{"role": "user", "content": "hello"})
		}},
		{"developer", "developer message", func(c string) obj {
			return base(obj{"role": "developer", "content": "owner " + c}, obj{"role": "user", "content": "hello"})
		}},
		{"assistant", "assistant message", func(c string) obj {
			return base(obj{"role": "user", "content": "hello"}, obj{"role": "assistant", "content": "sent to " + c}, obj{"role": "user", "content": "thanks"})
		}},
		{"refusal", "assistant refusal", func(c string) obj {
			return base(obj{"role": "user", "content": "hello"}, obj{"role": "assistant", "content": nil, "refusal": "cannot help " + c}, obj{"role": "user", "content": "ok"})
		}},
		{"tool-args-value", "tool_calls[].function.arguments (value)", func(c string) obj {
			return base(toolCallMsgs(argsJSON(obj{"to": c}))...)
		}},
		{"tool-args-key", "tool_calls[].function.arguments (key)", func(c string) obj {
			return base(toolCallMsgs(argsJSON(obj{c: 1}))...)
		}},
		{"tool-result", "tool message content", func(c string) obj {
			return base(obj{"role": "user", "content": "hello"},
				obj{"role": "assistant", "content": nil, "tool_calls": []any{obj{"id": "call_1", "type": "function", "function": obj{"name": "lookup", "arguments": "{}"}}}},
				obj{"role": "tool", "tool_call_id": "call_1", "content": "found " + c})
		}},
		{"tool-description", "tools[].function.description", func(c string) obj {
			return with(base(), "tools", []any{obj{"type": "function", "function": obj{"name": "lookup", "description": "ask " + c, "parameters": obj{"type": "object"}}}})
		}},
		{"tool-schema", "tools[].function.parameters (schema text)", func(c string) obj {
			return with(base(), "tools", []any{obj{"type": "function", "function": obj{"name": "lookup", "parameters": obj{
				"type": "object", "properties": obj{"to": obj{"type": "string", "description": "e.g. " + c}}}}}})
		}},
		{"functions-legacy", "functions[].description (legacy)", func(c string) obj {
			return with(base(), "functions", []any{obj{"name": "lookup", "description": "ask " + c, "parameters": obj{"type": "object"}}})
		}},
		{"user", "user", func(c string) obj { return with(base(), "user", c) }},
		{"metadata-value", "metadata (value)", func(c string) obj { return with(base(), "metadata", obj{"k": c}) }},
		{"metadata-key", "metadata (key)", func(c string) obj { return with(base(), "metadata", obj{c: "v"}) }},
		{"stop", "stop", func(c string) obj { return with(base(), "stop", []any{c}) }},
		{"prediction", "prediction.content", func(c string) obj {
			return with(base(), "prediction", obj{"type": "content", "content": c})
		}},
		{"response-format", "response_format.json_schema.description", func(c string) obj {
			return with(base(), "response_format", obj{"type": "json_schema", "json_schema": obj{
				"name": "r", "description": "about " + c, "schema": obj{"type": "object"}}})
		}},
		{"image-url", "image_url.url (http, query string)", func(c string) obj {
			return base(obj{"role": "user", "content": []any{obj{"type": "text", "text": "see"},
				obj{"type": "image_url", "image_url": obj{"url": "https://img.test/a.png?e=" + c}}}})
		}},
		{"unknown-field", "unknown top-level field", func(c string) obj {
			return with(base(), "x_vendor_extension", obj{"note": c})
		}},
		{"unknown-field-key", "unknown top-level field (name)", func(c string) obj {
			return with(base(), c, "v")
		}},
	}
}

// forwardKey names, for cases that live in one top-level request key, the key
// whose absence from the upstream body means the proxy removed the field. Such a
// proxy leaks nothing, but it has not shown that it can mask the field, and the
// request the upstream sees is not the one the client sent.
var forwardKey = map[string]string{
	"tool-description": "tools", "tool-schema": "tools", "functions-legacy": "functions",
	"user": "user", "metadata-value": "metadata", "metadata-key": "metadata", "stop": "stop",
	"prediction": "prediction", "response-format": "response_format", "unknown-field": "x_vendor_extension",
}

// FieldResult is the outcome for one case.
type FieldResult struct {
	ID      string  `json:"id"`
	Field   string  `json:"field"`
	Outcome Outcome `json:"outcome"`
	Status  int     `json:"status"`
}

// FieldReport is the matrix column for one proxy.
type FieldReport struct {
	Label   string        `json:"label"`
	Results []FieldResult `json:"results"`
}

// RunFields plants a distinct canary email in each field, sends the request,
// and checks whether the canary is still in the body the upstream received.
func RunFields(ctx context.Context, t Target, runID, label string) FieldReport {
	rep := FieldReport{Label: label}
	for i, c := range Cases() {
		// The domain must be one a validating detector accepts: Presidio's email
		// recognizer checks the TLD against the public suffix list, so a made-up
		// ".test" address would "leak" there only because nothing treats it as an email.
		canary := fmt.Sprintf("canary%02d@example.com", i)
		id := fmt.Sprintf("%s-field-%s", runID, c.ID)
		body, _ := json.Marshal(c.Build(canary))
		res := FieldResult{ID: c.ID, Field: c.Field}
		status, err := t.send(ctx, id, body)
		res.Status = status
		switch {
		case err != nil:
			res.Outcome = Failed
		case status != http.StatusOK:
			res.Outcome = Rejected
		default:
			got, err := t.received(ctx, id)
			switch {
			case err != nil:
				res.Outcome = Failed
			case strings.Contains(string(got), canary):
				res.Outcome = Leaked
			case forwardKey[c.ID] != "" && !hasTopKey(got, forwardKey[c.ID]):
				res.Outcome = Dropped
			default:
				res.Outcome = Masked
			}
		}
		rep.Results = append(rep.Results, res)
	}
	return rep
}

func hasTopKey(body []byte, key string) bool {
	v, err := jsonx.Decode(body)
	if err != nil {
		return true // cannot tell; do not claim a drop
	}
	m, _ := v.(map[string]any)
	_, ok := m[key]
	return ok
}

// FieldsMarkdown renders several proxies side by side, one row per field.
func FieldsMarkdown(reports []FieldReport) string {
	if len(reports) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("| request field |")
	for _, r := range reports {
		sb.WriteString(" " + r.Label + " |")
	}
	sb.WriteString("\n|---|")
	for range reports {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")
	for i, c := range reports[0].Results {
		fmt.Fprintf(&sb, "| %s |", c.Field)
		for _, r := range reports {
			if i < len(r.Results) {
				fmt.Fprintf(&sb, " %s |", r.Results[i].Outcome)
			} else {
				sb.WriteString(" - |")
			}
		}
		sb.WriteString("\n")
	}
	var leaks []string
	for _, r := range reports {
		n := 0
		for _, x := range r.Results {
			if x.Outcome == Leaked {
				n++
			}
		}
		leaks = append(leaks, fmt.Sprintf("%s %d/%d", r.Label, n, len(r.Results)))
	}
	sort.Strings(leaks)
	fmt.Fprintf(&sb, "\nFields where the canary reached the upstream: %s.\n", strings.Join(leaks, ", "))
	var drops []string
	for _, r := range reports {
		n := 0
		for _, x := range r.Results {
			if x.Outcome == Dropped {
				n++
			}
		}
		if n > 0 {
			drops = append(drops, fmt.Sprintf("%s %d", r.Label, n))
		}
	}
	if len(drops) > 0 {
		sort.Strings(drops)
		fmt.Fprintf(&sb, "Fields the proxy removed instead of masking (counted as not leaked): %s.\n", strings.Join(drops, ", "))
	}
	return sb.String()
}
