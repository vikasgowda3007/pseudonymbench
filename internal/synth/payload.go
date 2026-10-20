package synth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PayloadSets lists the request shapes ChatBody can build.
var PayloadSets = []string{"plain", "pii", "pii-long", "tools"}

// ChatBody builds one chat-completions request body from a named set. Every
// call draws fresh PII values, so a run does not hammer a single vault entry.
//
//	plain     no PII: the cost of scanning clean text
//	pii       one message with an email, phone, card, SSN and IBAN
//	pii-long  a system prompt plus a multi-turn history with about twenty items
//	tools     PII in a user message, in tool-call arguments and in a tool result
func (g *Gen) ChatBody(set string, stream bool) ([]byte, error) {
	var msgs []any
	user := func(s string) any { return map[string]any{"role": "user", "content": s} }
	switch set {
	case "plain":
		msgs = []any{user("Summarize the main trade-offs between optimistic and pessimistic locking in a relational database, " +
			"and give one example where each is the better choice. Keep it under one hundred words.")}
	case "pii":
		msgs = []any{user(fmt.Sprintf(
			"Please email %s and call %s about the overdue invoice. Charge card %s. Tax ID %s. Refund to %s.",
			g.Email(), g.Phone(), g.Card(), g.SSN(), g.IBAN()))}
	case "pii-long":
		msgs = []any{map[string]any{"role": "system", "content": "You are a careful assistant for a billing team. " + strings.Repeat("Follow the policy. ", 20)}}
		for t := 0; t < 4; t++ {
			msgs = append(msgs, user(fmt.Sprintf(
				"Customer %s (%s) disputes a charge on %s. Their account %s and tax ID %s are on file; escalate to %s if unresolved.",
				g.Email(), g.Phone(), g.Card(), g.IBAN(), g.SSN(), g.Email())))
			msgs = append(msgs, map[string]any{"role": "assistant", "content": "Understood. I have noted the dispute and will follow up."})
		}
		msgs = append(msgs, user("Draft the reply."))
	case "tools":
		email, phone := g.Email(), g.Phone()
		args, _ := json.Marshal(map[string]any{"to": email, "phone": phone, "subject": "Invoice"})
		msgs = []any{
			user(fmt.Sprintf("Look up %s and send the receipt.", email)),
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function",
				"function": map[string]any{"name": "send_receipt", "arguments": string(args)}}}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "content": fmt.Sprintf("sent; card %s, tax id %s", g.Card(), g.SSN())},
			user("Thanks. Anything else on file?"),
		}
	default:
		return nil, fmt.Errorf("unknown payload set %q (have %v)", set, PayloadSets)
	}
	body := map[string]any{"model": "mock-model", "messages": msgs, "stream": stream}
	if set == "tools" {
		body["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "send_receipt", "description": "Email a receipt to a customer",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"to": map[string]any{"type": "string"}}},
		}}}
	}
	return json.Marshal(body)
}
