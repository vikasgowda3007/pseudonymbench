package detect

import (
	"strings"
	"testing"
)

func BenchmarkFinders(b *testing.B) {
	s := strings.Repeat("Hello, please email john@acme.com about order 20261019123456 and card 4111 1111 1111 1111. ", 20)
	for _, k := range Kinds {
		b.Run(string(k), func(b *testing.B) {
			b.SetBytes(int64(len(s)))
			for i := 0; i < b.N; i++ {
				finders[k](s)
			}
		})
	}
}
