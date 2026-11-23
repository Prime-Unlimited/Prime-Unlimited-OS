package usbcopy

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func mpegFrame(index, kbps int) []byte {
	n := 144000 * kbps / 44100
	b := make([]byte, n)
	copy(b, []byte{255, 251, byte(index << 4), 0})
	return b
}
func TestMP3MeasuresCBRAndSkipsTags(t *testing.T) {
	root := t.TempDir()
	data := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0}
	for i := 0; i < 100; i++ {
		data = append(data, mpegFrame(9, 128)...)
	}
	tail := make([]byte, 128)
	copy(tail, "TAG")
	data = append(data, tail...)
	if e := os.WriteFile(filepath.Join(root, "song.mp3"), data, 0600); e != nil {
		t.Fatal(e)
	}
	p, e := ReadMP3Properties(context.Background(), root, "song.mp3")
	if e != nil {
		t.Fatal(e)
	}
	if p.Frames != 100 || math.Abs(p.Duration.Seconds()-100*1152.0/44100) > 0.001 || p.BitrateKbps != 128 {
		t.Fatalf("incorrect properties: %+v", p)
	}
}
func TestMP3MeasuresVariableBitrate(t *testing.T) {
	root := t.TempDir()
	data := []byte{}
	for i := 0; i < 50; i++ {
		data = append(data, mpegFrame(9, 128)...)
		data = append(data, mpegFrame(11, 192)...)
	}
	os.WriteFile(filepath.Join(root, "vbr.mp3"), data, 0600)
	p, e := ReadMP3Properties(context.Background(), root, "vbr.mp3")
	if e != nil {
		t.Fatal(e)
	}
	if p.Frames != 100 || p.BitrateKbps != 160 {
		t.Fatalf("incorrect VBR properties: %+v", p)
	}
}
func TestMP3RejectsTruncationAndCancelledRead(t *testing.T) {
	root := t.TempDir()
	data := append(mpegFrame(9, 128), mpegFrame(9, 128)[:100]...)
	os.WriteFile(filepath.Join(root, "broken.mp3"), data, 0600)
	if _, e := ReadMP3Properties(context.Background(), root, "broken.mp3"); e == nil {
		t.Fatal("truncated stream accepted")
	}
	os.WriteFile(filepath.Join(root, "valid.mp3"), append(mpegFrame(9, 128), mpegFrame(9, 128)...), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := ReadMP3Properties(ctx, root, "valid.mp3"); e == nil {
		t.Fatal("cancellation ignored")
	}
}

func apeTrailerFixture(withHeader bool) []byte {
	// One UTF-8 value; the MP3 byte-range calculation must not count it as audio.
	payload := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	payload = append(payload, []byte("Title\x00APE")...)
	footer := make([]byte, 32)
	copy(footer, "APETAGEX")
	binary.LittleEndian.PutUint32(footer[8:], 2000)
	binary.LittleEndian.PutUint32(footer[12:], uint32(len(payload)+32))
	binary.LittleEndian.PutUint32(footer[16:], 1)
	if withHeader {
		binary.LittleEndian.PutUint32(footer[20:], 1<<31)
		header := append([]byte(nil), footer...)
		binary.LittleEndian.PutUint32(header[20:], (1<<31)|(1<<29))
		return append(append(header, payload...), footer...)
	}
	return append(payload, footer...)
}
func TestMP3APETrailersDoNotChangeMeasuredAudio(t *testing.T) {
	root := t.TempDir()
	audio := []byte{}
	for i := 0; i < 100; i++ {
		audio = append(audio, mpegFrame(9, 128)...)
	}
	for _, header := range []bool{false, true} {
		for _, id3 := range []bool{false, true} {
			data := append(append([]byte(nil), audio...), apeTrailerFixture(header)...)
			if id3 {
				tail := make([]byte, 128)
				copy(tail, "TAG")
				data = append(data, tail...)
			}
			if e := os.WriteFile(filepath.Join(root, "ape.mp3"), data, 0600); e != nil {
				t.Fatal(e)
			}
			p, e := ReadMP3Properties(context.Background(), root, "ape.mp3")
			if e != nil || p.Frames != 100 || p.BitrateKbps != 128 {
				t.Fatalf("header=%v ID3v1=%v: %+v %v", header, id3, p, e)
			}
		}
	}
}
func TestMP3APETruncationAndMalformedTrailersAreRejected(t *testing.T) {
	audio := append(mpegFrame(9, 128), mpegFrame(9, 128)...)
	cases := map[string][]byte{}
	cases["truncated-audio.mp3"] = append(append([]byte(nil), audio[:len(audio)-100]...), apeTrailerFixture(true)...)
	a := apeTrailerFixture(true)
	a[len(a)-12] |= 32
	cases["invalid-flags.mp3"] = append(append([]byte(nil), audio...), a...)
	a = apeTrailerFixture(false)
	binary.LittleEndian.PutUint32(a[len(a)-20:], 0xffffffff)
	cases["invalid-size.mp3"] = append(append([]byte(nil), audio...), a...)
	a = apeTrailerFixture(false)
	binary.LittleEndian.PutUint32(a[len(a)-16:], 2)
	cases["invalid-count.mp3"] = append(append([]byte(nil), audio...), a...)
	a = apeTrailerFixture(true)
	a[8] = 0
	cases["header-mismatch.mp3"] = append(append([]byte(nil), audio...), a...)
	a = apeTrailerFixture(false)
	a[13] = 255
	cases["invalid-key.mp3"] = append(append([]byte(nil), audio...), a...)
	root := t.TempDir()
	for name, data := range cases {
		if e := os.WriteFile(filepath.Join(root, name), data, 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := ReadMP3Properties(context.Background(), root, name); e == nil {
			t.Fatal("accepted malformed MP3/APE", name)
		}
	}
}
func TestOwnerProvidedMP3PreflightFixture(t *testing.T) {
	path := os.Getenv("PRIME_USB_MP3_PREFLIGHT_FIXTURE")
	if path == "" {
		t.Skip("explicit read-only fixture required")
	}
	p, e := ReadMP3Properties(context.Background(), filepath.Dir(path), filepath.Base(path))
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("audio frames=%d duration=%s bitrate=%d kbps", p.Frames, p.Duration, p.BitrateKbps)
}
