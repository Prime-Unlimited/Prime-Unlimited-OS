package usbcopy

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

// EngineArtworkIdentity derives the Database2 hash and artwork filename from
// the original embedded image. Engine Desktop's exported JPEG is a derivative;
// hashing that JPEG would produce a different identity. This mapping was
// verified against all ten artwork records in the laboratory export.
func EngineArtworkIdentity(original []byte) ([]byte, string, error) {
	if len(original) == 0 || len(original) > maxArtworkBytes {
		return nil, "", errors.New("embedded artwork size is invalid")
	}
	digest := sha1.Sum(original)
	hash := append([]byte(nil), digest[:]...)
	return hash, base64.RawURLEncoding.EncodeToString(hash) + ".jpg", nil
}

// PreparedArtwork is an in-memory candidate for the observed external artwork
// layout. Encoding and scaling parity with Engine OS are not yet validated.
type PreparedArtwork struct {
	Hash     []byte
	Filename string
	JPEG     []byte
}

// PrepareEngineArtwork never writes files. Non-square images are fitted onto
// a black square without cropping. Decode dimensions are bounded before decode.
func PrepareEngineArtwork(original []byte) (PreparedArtwork, error) {
	hash, name, err := EngineArtworkIdentity(original)
	if err != nil {
		return PreparedArtwork{}, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(original))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return PreparedArtwork{}, errors.New("embedded artwork dimensions are unsupported")
	}
	src, _, err := image.Decode(bytes.NewReader(original))
	if err != nil {
		return PreparedArtwork{}, errors.New("embedded artwork could not be decoded")
	}
	const size = 256
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dst.SetRGBA(x, y, color.RGBA{A: 255})
		}
	}
	width, height := size, size
	if cfg.Width > cfg.Height {
		height = cfg.Height * size / cfg.Width
	} else {
		width = cfg.Width * size / cfg.Height
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	bounds := src.Bounds()
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			sy := bounds.Min.Y + y*bounds.Dy()/height
			// Composite transparent source pixels on the black canvas.
			c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
			a := uint32(c.A)
			dst.SetRGBA((size-width)/2+x, (size-height)/2+y, color.RGBA{R: uint8(uint32(c.R) * a / 255), G: uint8(uint32(c.G) * a / 255), B: uint8(uint32(c.B) * a / 255), A: 255})
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 90}); err != nil {
		return PreparedArtwork{}, err
	}
	return PreparedArtwork{Hash: hash, Filename: name, JPEG: out.Bytes()}, nil
}
