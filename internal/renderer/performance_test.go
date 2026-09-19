package renderer

import (
	"encoding/binary"
	"math"
	"testing"
)

func BenchmarkParseWKBPoint(b *testing.B) {
	wkb := make([]byte, 21)
	wkb[0] = 1
	binary.LittleEndian.PutUint32(wkb[1:5], 1)
	binary.LittleEndian.PutUint64(wkb[5:13], math.Float64bits(13.4))
	binary.LittleEndian.PutUint64(wkb[13:21], math.Float64bits(52.5))
	b.ReportAllocs()
	for b.Loop() {
		geom, err := ParseWKB(wkb)
		if err != nil || geom.VertexCount() != 1 {
			b.Fatal(err)
		}
	}
}
