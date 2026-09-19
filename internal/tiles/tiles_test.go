package tiles

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"testing"

	"github.com/HugoSmits86/nativewebp"
	"github.com/paulmach/orb/encoding/mvt"
)

func TestEncodeFeaturesToMVT(t *testing.T) {
	generator := NewMVTGenerator(4096)
	raw := []byte(`{"type":"Feature","id":7,"properties":{"name":"point"},"geometry":{"type":"Point","coordinates":[0,0]}}`)
	data, err := generator.encodeFeaturesToMVT([]json.RawMessage{raw}, "points", &TileBounds{MinX: -1, MinY: -1, MaxX: 1, MaxY: 1})
	if err != nil {
		t.Fatal(err)
	}
	layers, err := mvt.Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 1 || len(layers[0].Features) != 1 {
		t.Fatalf("unexpected tile: %#v", layers)
	}
	if layers[0].Features[0].ID != float64(7) {
		t.Fatalf("numeric ID not preserved: %#v", layers[0].Features[0].ID)
	}
}

func TestWebPEncodingIsRealWebP(t *testing.T) {
	generator := NewMapTileGenerator(2)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	data, err := generator.encodeImage(img, TileFormatWEBP)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Fatalf("not WebP: %x", data[:min(12, len(data))])
	}
	if _, err := nativewebp.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("decode WebP: %v", err)
	}
}

func TestParseTileFormatRejectsUnknown(t *testing.T) {
	if _, err := ParseTileFormat("gif"); err == nil {
		t.Fatal("expected unsupported format error")
	}
}
