package mask

import (
	"fmt"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// ParsePolicy reads "CARD=block,PHONE=off,EMAIL=mask". Kinds not listed are
// masked. An empty string gives the default policy.
func ParsePolicy(s string) (Policy, error) {
	p := Policy{}
	if strings.TrimSpace(s) == "" {
		return p, nil
	}
	known := map[string]detect.Kind{}
	for _, k := range detect.Kinds {
		known[string(k)] = k
	}
	for _, part := range strings.Split(s, ",") {
		name, act, ok := strings.Cut(strings.TrimSpace(part), "=")
		kind, kok := known[strings.ToUpper(strings.TrimSpace(name))]
		if !ok || !kok {
			return nil, fmt.Errorf("policy entry %q: want KIND=mask|block|off with KIND one of %v", part, detect.Kinds)
		}
		switch strings.ToLower(strings.TrimSpace(act)) {
		case "mask":
			p[kind] = ActionMask
		case "block":
			p[kind] = ActionBlock
		case "off":
			p[kind] = ActionOff
		default:
			return nil, fmt.Errorf("policy entry %q: action must be mask, block or off", part)
		}
	}
	return p, nil
}
