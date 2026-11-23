package usbcopy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/dhowden/tag"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RIFF/WAVE may carry embedded ID3 chunks or a LIST/INFO dictionary. Prefer
// explicit ID3 fields over INFO fields, independent of chunk order. This reads
// metadata only; audio and source database bytes are never changed.
func readWaveMetadata(file *os.File, track *Track) error {
	st, e := file.Stat()
	if e != nil {
		return e
	}
	read := func(at int64, n int) ([]byte, error) {
		if at < 0 || n < 0 || int64(n) > st.Size()-at {
			return nil, io.ErrUnexpectedEOF
		}
		b := make([]byte, n)
		_, e := file.ReadAt(b, at)
		return b, e
	}
	h, e := read(0, 12)
	if e != nil {
		return e
	}
	if string(h[:4]) != "RIFF" || string(h[8:]) != "WAVE" {
		return errors.New("unrecognized WAVE metadata container")
	}
	end := int64(binary.LittleEndian.Uint32(h[4:8])) + 8
	if end < 12 || end > st.Size() {
		return errors.New("WAVE metadata bounds are invalid")
	}
	info := Tags{Format: "WAV"}
	var id3 *Track
	for off, count := int64(12), 0; off < end; count++ {
		if count >= 10000 || end-off < 8 {
			return errors.New("WAVE metadata chunk boundary invalid")
		}
		h, e = read(off, 8)
		if e != nil {
			return e
		}
		n := int64(binary.LittleEndian.Uint32(h[4:]))
		start := off + 8
		if n > end-start {
			return io.ErrUnexpectedEOF
		}
		switch {
		case strings.EqualFold(string(h[:4]), "id3 "):
			if id3 != nil || n < 10 || n > 64<<20 {
				return errors.New("ambiguous or oversized WAVE ID3 metadata")
			}
			metadata, e := tag.ReadFrom(io.NewSectionReader(file, start, n))
			if e != nil {
				return e
			}
			parsed := &Track{}
			if e = applyAudioMetadata(metadata, parsed); e != nil {
				return e
			}
			id3 = parsed
		case string(h[:4]) == "LIST" && n >= 4:
			kind, e := read(start, 4)
			if e != nil {
				return e
			}
			if string(kind) == "INFO" {
				if n > 1<<20 {
					return errors.New("WAVE INFO exceeds metadata limit")
				}
				data, e := read(start+4, int(n-4))
				if e != nil {
					return e
				}
				if e = readWaveInfo(data, &info); e != nil {
					return e
				}
			}
		}
		off = start + n
		if n&1 != 0 && !(off == end && string(h[:4]) == "data") {
			off++
		}
		if off > end {
			return io.ErrUnexpectedEOF
		}
	}
	track.Tags = info
	if id3 != nil {
		merged := id3.Tags
		// Native tag readers also permit INFO as fallback for missing ID3 fields.
		fields := []struct {
			dst      *string
			fallback string
		}{{&merged.Title, info.Title}, {&merged.Artist, info.Artist}, {&merged.Album, info.Album}, {&merged.Genre, info.Genre}, {&merged.Comment, info.Comment}}
		for _, field := range fields {
			if *field.dst == "" {
				*field.dst = field.fallback
			}
		}
		if merged.Year == 0 {
			merged.Year = info.Year
		}
		if merged.Track == 0 {
			merged.Track = info.Track
			merged.TrackTotal = info.TrackTotal
		}
		track.Tags = merged
		track.Artwork = id3.Artwork
		track.ArtworkHash = id3.ArtworkHash
		track.ArtworkFilename = id3.ArtworkFilename
	}
	return nil
}
func readWaveInfo(data []byte, tags *Tags) error {
	for off := 0; off < len(data); {
		if len(data)-off < 8 {
			return io.ErrUnexpectedEOF
		}
		id := string(data[off : off+4])
		n := int64(binary.LittleEndian.Uint32(data[off+4:]))
		off += 8
		if n > int64(len(data)-off) {
			return io.ErrUnexpectedEOF
		}
		raw := bytes.TrimRight(data[off:off+int(n)], "\x00")
		var text string
		if utf8.Valid(raw) {
			text = string(raw)
		} else {
			r := make([]rune, len(raw))
			for i, b := range raw {
				r[i] = rune(b)
			}
			text = string(r)
		}
		text = cleanTag(text)
		switch id {
		case "INAM":
			tags.Title = text
		case "IART":
			tags.Artist = text
		case "IPRD":
			tags.Album = text
		case "IGNR":
			tags.Genre = text
		case "ICMT":
			tags.Comment = text
		case "ICRD":
			if len(text) >= 4 {
				tags.Year, _ = strconv.Atoi(text[:4])
			}
		case "ITRK":
			parts := strings.SplitN(text, "/", 2)
			tags.Track, _ = strconv.Atoi(parts[0])
			if len(parts) == 2 {
				tags.TrackTotal, _ = strconv.Atoi(parts[1])
			}
		}
		off += int(n) + (int(n) & 1)
		if off > len(data) {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}
