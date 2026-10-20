package mask

import (
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

func TestParsePolicy(t *testing.T) {
	p, err := ParsePolicy("card=block, PHONE=off,EMAIL=mask")
	if err != nil {
		t.Fatal(err)
	}
	if p.action(detect.Card) != ActionBlock || p.action(detect.Phone) != ActionOff ||
		p.action(detect.Email) != ActionMask || p.action(detect.SSN) != ActionMask {
		t.Errorf("policy = %v", p)
	}
	if p, err := ParsePolicy(""); err != nil || len(p) != 0 {
		t.Errorf("empty = %v, %v", p, err)
	}
	for _, bad := range []string{"CARD", "CARD=hash", "NAME=mask", "CARD=block,,"} {
		if _, err := ParsePolicy(bad); err == nil {
			t.Errorf("ParsePolicy(%q) accepted", bad)
		}
	}
}
