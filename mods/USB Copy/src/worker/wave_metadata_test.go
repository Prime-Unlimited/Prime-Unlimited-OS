package usbcopy

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func appendWaveChunk(data []byte, id string, payload []byte) []byte {
	out := append([]byte(nil), data...)
	out = append(out, []byte(id)...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(payload)))
	out = append(out, payload...)
	if len(payload)&1 != 0 {
		out = append(out, 0)
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}
func infoFixture(fields ...string) []byte {
	out := []byte("INFO")
	for i := 0; i < len(fields); i += 2 {
		out = append(out, []byte(fields[i])...)
		value := append([]byte(fields[i+1]), 0)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(value)))
		out = append(out, value...)
		if len(value)&1 != 0 {
			out = append(out, 0)
		}
	}
	return out
}
func id3WaveFixture() []byte {
	body := []byte{}
	for _, f := range []struct{ id, value string }{{"TIT2", "Embedded WAV title"}, {"TPE1", "Embedded artist"}} {
		body = append(body, []byte(f.id)...)
		value := append([]byte{0}, []byte(f.value)...)
		body = binary.BigEndian.AppendUint32(body, uint32(len(value)))
		body = append(body, 0, 0)
		body = append(body, value...)
	}
	n := len(body)
	header := []byte{'I', 'D', '3', 3, 0, 0, byte(n>>21) & 127, byte(n>>14) & 127, byte(n>>7) & 127, byte(n) & 127}
	return append(header, body...)
}
func TestScanReadsWaveInfoAndEmbeddedID3WithoutChangingAudio(t *testing.T) {
	for _, id3First := range []bool{false, true} {
		root := t.TempDir()
		data := waveFixture(1, 24, true)
		info := infoFixture("INAM", "INFO title", "IART", "INFO artist", "IPRD", "Album", "ICRD", "2024-03-10", "ITRK", "3/12")
		if id3First {
			data = appendWaveChunk(data, "ID3 ", id3WaveFixture())
			data = appendWaveChunk(data, "LIST", info)
		} else {
			data = appendWaveChunk(data, "LIST", info)
			data = appendWaveChunk(data, "id3 ", id3WaveFixture())
		}
		path := filepath.Join(root, "track.wav")
		if e := os.WriteFile(path, data, 0600); e != nil {
			t.Fatal(e)
		}
		snapshot, e := Scan(root)
		if e != nil {
			t.Fatal(e)
		}
		if len(snapshot.Tracks) != 1 {
			t.Fatal("missing WAV")
		}
		tr := snapshot.Tracks[0]
		if tr.Tags.Title != "Embedded WAV title" || tr.Tags.Artist != "Embedded artist" || tr.Tags.Album != "Album" || tr.Tags.Year != 2024 || tr.Tags.Track != 3 || tr.Tags.TrackTotal != 12 {
			t.Fatalf("WAV metadata lost: %+v", tr.Tags)
		}
		after, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(after, data) {
			t.Fatal("source WAV changed", e)
		}
	}
}
func TestWaveInfoAndTaglessFallbackRemainDistinct(t *testing.T) {
	root := t.TempDir()
	data := appendWaveChunk(waveFixture(1, 16, false), "LIST", infoFixture("INAM", "Täst title", "IART", "Artist"))
	if e := os.WriteFile(filepath.Join(root, "tagged.wav"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "plain.wav"), waveFixture(1, 16, false), 0600); e != nil {
		t.Fatal(e)
	}
	snapshot, e := Scan(root)
	if e != nil {
		t.Fatal(e)
	}
	for _, tr := range snapshot.Tracks {
		if tr.RelativePath == "plain.wav" {
			if tr.Tags.Title != "" {
				t.Fatal("invented embedded title")
			}
		} else if tr.Tags.Title != "Täst title" || tr.Tags.Artist != "Artist" {
			t.Fatal("INFO metadata not read", tr.Tags)
		}
	}
}
func TestWaveInfoRejectsOutOfBoundsItems(t *testing.T) {
	data := infoFixture("INAM", "Title")[4:]
	binary.LittleEndian.PutUint32(data[4:], 0xffffffff)
	var tags Tags
	if e := readWaveInfo(data, &tags); e == nil {
		t.Fatal("unbounded INFO item accepted")
	}
}
