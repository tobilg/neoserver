package mgmt

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateStyleAsset(t *testing.T) {
	var body bytes.Buffer
	value := image.NewRGBA(image.Rect(0, 0, 1, 1))
	value.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&body, value); err != nil {
		t.Fatal(err)
	}
	mediaType, err := validateStyleAsset(body.Bytes(), "image/png", 64)
	if err != nil || mediaType != "image/png" {
		t.Fatalf("type=%q err=%v", mediaType, err)
	}
	if _, err = validateStyleAsset([]byte("not an image"), "application/octet-stream", 64); err == nil {
		t.Fatal("invalid asset accepted")
	}
}

func TestStyleBodyCompatibilityAndLimit(t *testing.T) {
	format, body, err := resolveAuthoredStyleBody("", "", `<StyledLayerDescriptor/>`)
	if err != nil || format != "sld_1.1.0" || body == "" {
		t.Fatalf("format=%q body=%q err=%v", format, body, err)
	}
	if _, _, err = resolveAuthoredStyleBody("css", "", "*{}"); err == nil {
		t.Fatal("legacy body accepted for CSS")
	}
	h := &handler{}
	h.cfg.WMS.MaxStyleBodyBytes = 3
	if err = h.validateStyleBodySize("four"); err == nil {
		t.Fatal("oversized body accepted")
	}
}
