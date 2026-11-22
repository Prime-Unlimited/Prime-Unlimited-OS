package usbcopy

import (
	"context"
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
