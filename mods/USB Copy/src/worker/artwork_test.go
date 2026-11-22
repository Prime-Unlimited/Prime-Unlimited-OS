package usbcopy

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestArtworkRejectsMalformedAndOversizedImages(t *testing.T) {
	for _, input := range [][]byte{nil, []byte("not an image"), make([]byte, maxArtworkBytes+1)} {
		if _, err := PrepareEngineArtwork(input); err == nil {
			t.Fatal("invalid artwork accepted")
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 8193, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareEngineArtwork(b.Bytes()); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}

func TestArtworkFitsRectangleAndCompositesTransparency(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 128})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareEngineArtwork(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(prepared.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 256 || decoded.Bounds().Dy() != 256 {
		t.Fatal("wrong output dimensions")
	}
	red, green, blue, _ := decoded.At(128, 128).RGBA()
	if red < 110*257 || red > 145*257 || green > 10*257 || blue > 10*257 {
		t.Fatal("transparent pixel was not composited on black")
	}
	red, green, blue, _ = decoded.At(128, 10).RGBA()
	if red > 10*257 || green > 10*257 || blue > 10*257 {
		t.Fatal("aspect ratio padding is not black")
	}
	hash, name, err := EngineArtworkIdentity(b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, prepared.Hash) || name != prepared.Filename {
		t.Fatal("rendering changed original artwork identity")
	}
}
