package usbcopy

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LiveAudioExtension is deliberately narrower than the read-only scanner.
func LiveAudioExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".wav", ".flac":
		return true
	}
	return false
}

// ValidateLiveAudioFile checks the source container before any copy/native call.
// It does not generate analysis values or guarantee decoding of compressed frames;
// Engine owns decoding/registration. MP3 retains the existing frame measurement.
func ValidateLiveAudioFile(ctx context.Context, root, relative string) error {
	ext := strings.ToLower(filepath.Ext(relative))
	if ext == ".mp3" {
		_, err := ReadMP3Properties(ctx, root, relative)
		return err
	}
	if !LiveAudioExtension(relative) {
		return errors.New("unsupported live audio extension")
	}
	rel, err := cleanRelative(relative)
	if err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !noSymlinkPath(root, path) {
		return errors.New("unsafe live audio path")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("live audio unavailable")
	}
	read := func(at int64, n int) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if at < 0 || n < 0 || int64(n) > before.Size()-at {
			return nil, io.ErrUnexpectedEOF
		}
		b := make([]byte, n)
		_, err := f.ReadAt(b, at)
		return b, err
	}
	if ext == ".wav" {
		err = validateWaveContainer(read, before.Size())
	} else {
		err = validateFLACContainer(read, before.Size())
	}
	if err != nil {
		return err
	}
	after, err := f.Stat()
	if err != nil {
		return err
	}
	current, err := os.Stat(path)
	if err != nil || !noSymlinkPath(root, path) || !os.SameFile(before, current) || before.Size() != after.Size() || before.Size() != current.Size() || !before.ModTime().Equal(after.ModTime()) || !before.ModTime().Equal(current.ModTime()) {
		return errors.New("live audio changed during validation")
	}
	return ctx.Err()
}

type audioReadAt func(int64, int) ([]byte, error)

// RIFF/WAVE PCM and IEEE float, including their extensible GUID forms.
// See Microsoft WAVEFORMATEX / WAVEFORMATEXTENSIBLE specifications.
func validateWaveContainer(read audioReadAt, size int64) error {
	fail := errors.New("unsupported or malformed RIFF/WAVE container")
	h, err := read(0, 12)
	if err != nil {
		return err
	}
	if string(h[:4]) != "RIFF" || string(h[8:]) != "WAVE" {
		return fail
	}
	end := int64(binary.LittleEndian.Uint32(h[4:8])) + 8
	if end < 12 || end > size {
		return fail
	}
	var format, channels, bits, align uint16
	var rate, byteRate uint32
	var dataBytes int64
	haveFmt, haveData := false, false
	off := int64(12)
	for chunks := 0; off < end; chunks++ {
		if chunks >= 10000 || end-off < 8 {
			return fail
		}
		h, err = read(off, 8)
		if err != nil {
			return err
		}
		n := int64(binary.LittleEndian.Uint32(h[4:]))
		start := off + 8
		if n > end-start {
			return fail
		}
		switch string(h[:4]) {
		case "fmt ":
			if haveFmt || n < 16 || n > 4096 {
				return fail
			}
			b, e := read(start, int(n))
			if e != nil {
				return e
			}
			format = binary.LittleEndian.Uint16(b)
			channels = binary.LittleEndian.Uint16(b[2:])
			rate = binary.LittleEndian.Uint32(b[4:])
			byteRate = binary.LittleEndian.Uint32(b[8:])
			align = binary.LittleEndian.Uint16(b[12:])
			bits = binary.LittleEndian.Uint16(b[14:])
			if format == 0xfffe {
				if n < 40 || binary.LittleEndian.Uint16(b[16:]) < 22 || int64(binary.LittleEndian.Uint16(b[16:]))+18 > n {
					return fail
				}
				validBits := binary.LittleEndian.Uint16(b[18:])
				if validBits == 0 || validBits > bits {
					return fail
				}
				// KSDATAFORMAT_SUBTYPE_PCM/IEEE_FLOAT share this GUID suffix.
				if !bytes.Equal(b[26:40], []byte{0, 0, 0, 0, 16, 0, 128, 0, 0, 170, 0, 56, 155, 113}) {
					return fail
				}
				format = binary.LittleEndian.Uint16(b[24:])
			}
			if channels < 1 || channels > 8 || rate < 8000 || rate > 384000 {
				return fail
			}
			if format == 1 {
				if bits != 8 && bits != 16 && bits != 24 && bits != 32 {
					return fail
				}
			} else if format == 3 {
				if bits != 32 && bits != 64 {
					return fail
				}
			} else {
				return fail
			}
			if uint32(align) != uint32(channels)*uint32(bits)/8 || byteRate != rate*uint32(align) {
				return fail
			}
			haveFmt = true
		case "data":
			if haveData || n == 0 {
				return fail
			}
			haveData = true
			dataBytes = n
		}
		off = start + n
		// Some legacy PCM WAVs omit only the final data chunk's alignment
		// byte. Its declared payload still has to end exactly at RIFF EOF.
		if n&1 != 0 && !(off == end && string(h[:4]) == "data") {
			off++
		}
		if off > end {
			return fail
		}
	}
	if !haveFmt || !haveData || align == 0 || dataBytes%int64(align) != 0 {
		return fail
	}
	return nil
}

// FLAC metadata/container preflight, per RFC 9639 sections 7/8. No invented
// duration/bitrate is inserted into Engine; unknown total sample counts are valid.
func validateFLACContainer(read audioReadAt, size int64) error {
	fail := errors.New("unsupported or malformed FLAC container")
	magic, err := read(0, 4)
	if err != nil {
		return err
	}
	if string(magic) != "fLaC" {
		return fail
	}
	off := int64(4)
	for block := 0; block < 10000; block++ {
		h, e := read(off, 4)
		if e != nil {
			return e
		}
		kind := h[0] & 127
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		off += 4
		if kind == 127 || n > size-off {
			return fail
		}
		if block == 0 {
			if kind != 0 || n != 34 {
				return fail
			}
			b, e := read(off, 34)
			if e != nil {
				return e
			}
			min, max := binary.BigEndian.Uint16(b), binary.BigEndian.Uint16(b[2:])
			packed := binary.BigEndian.Uint64(b[10:18])
			rate := packed >> 44
			bits := (packed>>36)&31 + 1
			if min < 16 || max < min || rate == 0 || rate > 655350 || bits < 4 || bits > 32 {
				return fail
			}
		} else if kind == 0 {
			return fail
		}
		off += n
		if h[0]&128 != 0 {
			frame, e := read(off, 4)
			if e != nil {
				return e
			}
			if frame[0] != 255 || frame[1]&254 != 248 || frame[2]>>4 == 0 || frame[2]&15 == 15 || frame[3]&1 != 0 {
				return fail
			}
			return nil
		}
	}
	return fail
}
