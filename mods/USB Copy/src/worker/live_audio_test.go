package usbcopy

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func waveFixture(format, bits uint16, extensible bool) []byte {
	fmt := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmt, format)
	binary.LittleEndian.PutUint16(fmt[2:], 2)
	binary.LittleEndian.PutUint32(fmt[4:], 44100)
	align := uint16(2 * bits / 8)
	binary.LittleEndian.PutUint32(fmt[8:], 44100*uint32(align))
	binary.LittleEndian.PutUint16(fmt[12:], align)
	binary.LittleEndian.PutUint16(fmt[14:], bits)
	if extensible {
		fmt = append(fmt, make([]byte, 24)...)
		binary.LittleEndian.PutUint16(fmt, 0xfffe)
		binary.LittleEndian.PutUint16(fmt[16:], 22)
		binary.LittleEndian.PutUint16(fmt[18:], bits)
		copy(fmt[24:], []byte{byte(format), 0, 0, 0, 0, 0, 16, 0, 128, 0, 0, 170, 0, 56, 155, 113})
	}
	out := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	out = binary.LittleEndian.AppendUint32(out, uint32(len(fmt)))
	out = append(out, fmt...)
	out = append(out, []byte("JUNK\x01\x00\x00\x00x\x00data")...)
	out = binary.LittleEndian.AppendUint32(out, uint32(align)*32)
	out = append(out, make([]byte, int(align)*32)...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}
func flacFixture() []byte {
	out := make([]byte, 42)
	copy(out, []byte{'f', 'L', 'a', 'C', 128, 0, 0, 34})
	binary.BigEndian.PutUint16(out[8:], 4096)
	binary.BigEndian.PutUint16(out[10:], 4096)
	binary.BigEndian.PutUint64(out[18:], uint64(44100)<<44|uint64(1)<<41|uint64(15)<<36|4096)
	// A frame header is sufficient for container preflight, not decoder validation.
	return append(out, 255, 248, 137, 24, 0, 0, 0, 0)
}
func TestLiveAudioContainerPreflight(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"pcm16.wav", waveFixture(1, 16, false), true}, {"pcm24.WAV", waveFixture(1, 24, true), true}, {"float.wav", waveFixture(3, 32, false), true}, {"float-ext.wav", waveFixture(3, 32, true), true}, {"music.flac", flacFixture(), true},
		{"disguised.wav", []byte("not a WAVE file but named as one"), false}, {"truncated.wav", waveFixture(1, 16, false)[:30], false}, {"compressed.wav", waveFixture(2, 16, false), false}, {"empty.flac", []byte("fLaC"), false}, {"truncated.flac", flacFixture()[:40], false}, {"wrong.flac", waveFixture(1, 16, false), false}, {"unsupported.aiff", waveFixture(1, 16, false), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0600); e != nil {
				t.Fatal(e)
			}
			e := ValidateLiveAudioFile(context.Background(), root, tc.name)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
		})
	}
}
func TestLiveAudioRejectsCorruptContainerFields(t *testing.T) {
	cases := map[string][]byte{}
	b := waveFixture(1, 16, false)
	b[32] = 1
	cases["alignment.wav"] = b
	b = waveFixture(1, 24, true)
	b[55] ^= 1
	cases["guid.wav"] = b
	b = waveFixture(1, 16, false)
	binary.LittleEndian.PutUint32(b[4:], 0xffffffff)
	cases["bounds.wav"] = b
	b = flacFixture()
	b[5] = 255
	cases["bounds.flac"] = b
	b = flacFixture()
	b[4] = 129
	cases["first-block.flac"] = b
	b = flacFixture()
	b[42] = 0
	cases["frame.flac"] = b
	b = flacFixture()
	binary.BigEndian.PutUint64(b[18:], 0)
	cases["sample-rate.flac"] = b
	root := t.TempDir()
	for name, b := range cases {
		if e := os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
		if e := ValidateLiveAudioFile(context.Background(), root, name); e == nil {
			t.Fatal("accepted malformed", name)
		}
	}
}
func TestLiveAudioRejectsSymlinkAndCancellation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "real.wav")
	if e := os.WriteFile(path, waveFixture(1, 16, false), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(path, filepath.Join(root, "link.wav")); e != nil {
		t.Fatal(e)
	}
	if e := ValidateLiveAudioFile(context.Background(), root, "link.wav"); e == nil {
		t.Fatal("accepted symlink")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := ValidateLiveAudioFile(ctx, root, "real.wav"); e == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestFLACUnknownSampleTotalAndMetadataBounds(t *testing.T) {
	b := flacFixture()
	packed := binary.BigEndian.Uint64(b[18:26])
	binary.BigEndian.PutUint64(b[18:26], packed & ^uint64((1<<36)-1))
	read := func(at int64, n int) ([]byte, error) {
		out := make([]byte, n)
		_, e := bytes.NewReader(b).ReadAt(out, at)
		return out, e
	}
	if e := validateFLACContainer(read, int64(len(b))); e != nil {
		t.Fatal("unknown total samples is valid", e)
	}
}

func TestLegacyOddLengthPCMDataAtRIFFEOF(t *testing.T) {
	b := waveFixture(1, 8, false)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 11025)
	binary.LittleEndian.PutUint32(b[28:], 11025)
	binary.LittleEndian.PutUint16(b[32:], 1)
	data := bytes.Index(b, []byte("data"))
	b = b[:len(b)-1]
	binary.LittleEndian.PutUint32(b[data+4:], uint32(len(b)-data-8))
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	read := func(at int64, n int) ([]byte, error) {
		out := make([]byte, n)
		_, e := bytes.NewReader(b).ReadAt(out, at)
		return out, e
	}
	if e := validateWaveContainer(read, int64(len(b))); e != nil {
		t.Fatal("legacy final odd PCM payload should remain readable", e)
	}
	// An unpadded odd chunk before another chunk cannot be treated as EOF.
	b = append(b, []byte("JUNK\x00\x00\x00\x00")...)
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	if e := validateWaveContainer(read, int64(len(b))); e == nil {
		t.Fatal("accepted ambiguous unpadded non-final chunk")
	}
}
