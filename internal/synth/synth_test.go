package synth

import (
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// Every generated value, alone, must be found as exactly one match of its own
// kind covering the whole string. A failure here is either a generator bug or
// a detector gap; both matter.
func TestGeneratedValuesAreDetected(t *testing.T) {
	g := New(1)
	for _, k := range detect.Kinds {
		fails := 0
		for i := 0; i < 2000; i++ {
			v := g.Value(k)
			ms := detect.Find(v, detect.AllKinds())
			if len(ms) != 1 || ms[0].Kind != k || ms[0].Start != 0 || ms[0].End != len(v) {
				if fails++; fails <= 5 {
					t.Errorf("%s value %q -> %v", k, v, ms)
				}
			}
		}
		if fails > 0 {
			t.Errorf("%s: %d/2000 generated values not detected", k, fails)
		}
	}
}

func TestSampleSpansAreExact(t *testing.T) {
	g := New(2)
	for _, s := range g.Dataset(500) {
		for _, sp := range s.Spans {
			v := s.Text[sp.Start:sp.End]
			ms := detect.Find(v, detect.AllKinds())
			if len(ms) != 1 || ms[0].Kind != sp.Kind {
				t.Fatalf("span %+v = %q is not a clean %s in %q", sp, v, sp.Kind, s.Text)
			}
		}
	}
}

func TestDatasetIsReproducible(t *testing.T) {
	a, b := New(7).Dataset(50), New(7).Dataset(50)
	for i := range a {
		if a[i].Text != b[i].Text {
			t.Fatalf("sample %d differs for the same seed", i)
		}
	}
	if c := New(8).Dataset(50); c[0].Text == a[0].Text && c[1].Text == a[1].Text {
		t.Error("different seeds gave the same data")
	}
}

// Look-alikes are allowed to fool the detector; this just reports how often,
// so a regression in either direction is visible in test logs.
func TestNegativeFalsePositiveRate(t *testing.T) {
	g := New(3)
	byName := map[string][2]int{}
	for i := 0; i < 5000; i++ {
		n := g.Negative()
		c := byName[n.Name]
		c[1]++
		if len(detect.Find(n.Text, detect.AllKinds())) > 0 {
			c[0]++
		}
		byName[n.Name] = c
	}
	for name, c := range byName {
		t.Logf("%-28s false positives %4d / %4d", name, c[0], c[1])
	}
}
