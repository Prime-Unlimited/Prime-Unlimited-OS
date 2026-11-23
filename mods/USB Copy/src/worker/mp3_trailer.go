package usbcopy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

// Per the APEv2 specification, trailing APE items/footer precede an optional
// ID3v1 tag; the APE size includes its footer but excludes its optional header.
// Validate the entire bounded item layout instead of treating metadata as MPEG
// frames or accepting arbitrary trailing garbage. No metadata is modified.
func mp3AudioEnd(f *os.File, end int64) (int64, error) {
	if end >= 128 {
		var h [3]byte
		if _, e := f.ReadAt(h[:], end-128); e != nil {
			return 0, e
		}
		if string(h[:]) == "TAG" {
			end -= 128
		}
	}
	if end < 32 {
		return end, nil
	}
	var footer [32]byte
	if _, e := f.ReadAt(footer[:], end-32); e != nil {
		return 0, e
	}
	if string(footer[:8]) != "APETAGEX" {
		return end, nil
	}
	fail := errors.New("malformed APE metadata trailer")
	version := binary.LittleEndian.Uint32(footer[8:])
	size := int64(binary.LittleEndian.Uint32(footer[12:]))
	items := binary.LittleEndian.Uint32(footer[16:])
	flags := binary.LittleEndian.Uint32(footer[20:])
	if (version != 1000 && version != 2000) || size < 32 || size > 64<<20 || size > end || items > 10000 || flags & ^uint32((1<<31)|1) != 0 || !bytes.Equal(footer[24:], make([]byte, 8)) {
		return 0, fail
	}
	start := end - size
	if flags&(1<<31) != 0 {
		if version != 2000 || start < 32 {
			return 0, fail
		}
		start -= 32
		var header [32]byte
		if _, e := f.ReadAt(header[:], start); e != nil {
			return 0, e
		}
		if !bytes.Equal(header[:20], footer[:20]) || binary.LittleEndian.Uint32(header[20:]) != flags|(1<<29) || !bytes.Equal(header[24:], footer[24:]) {
			return 0, fail
		}
	}
	reader := io.NewSectionReader(f, end-size, size-32)
	remaining := size - 32
	for item := uint32(0); item < items; item++ {
		if remaining < 10 {
			return 0, fail
		}
		var h [8]byte
		if _, e := io.ReadFull(reader, h[:]); e != nil {
			return 0, e
		}
		remaining -= 8
		n := int64(binary.LittleEndian.Uint32(h[:]))
		itemFlags := binary.LittleEndian.Uint32(h[4:])
		if itemFlags & ^uint32(7) != 0 || (itemFlags>>1)&3 == 3 {
			return 0, fail
		}
		keySize := 0
		for {
			if remaining < 1 || keySize > 255 {
				return 0, fail
			}
			var b [1]byte
			if _, e := io.ReadFull(reader, b[:]); e != nil {
				return 0, e
			}
			remaining--
			if b[0] == 0 {
				break
			}
			if b[0] < 32 || b[0] > 126 {
				return 0, fail
			}
			keySize++
		}
		if keySize < 2 || n > remaining {
			return 0, fail
		}
		if _, e := reader.Seek(n, io.SeekCurrent); e != nil {
			return 0, e
		}
		remaining -= n
	}
	if remaining != 0 {
		return 0, fail
	}
	return start, nil
}
