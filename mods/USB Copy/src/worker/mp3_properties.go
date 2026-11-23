package usbcopy

import (
	"context"
	"errors"
	mp3 "github.com/tcolgate/mp3"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

type MP3Properties struct {
	Duration        time.Duration
	DurationSeconds int
	BitrateKbps     int
	Frames          int
}
type cancelAudioReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r cancelAudioReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.reader.Read(p)
}

// ReadMP3Properties measures MPEG frames, not ID3 size or Engine analysis data.
// Supports constant and variable bitrate. It rejects truncated/corrupt streams
// rather than inventing duration from the total file size.
func ReadMP3Properties(ctx context.Context, root, relative string) (result MP3Properties, err error) {
	defer func() {
		if recover() != nil {
			result = MP3Properties{}
			err = errors.New("unsupported or malformed MPEG frames")
		}
	}()
	rel, e := cleanRelative(relative)
	if e != nil {
		return result, e
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !noSymlinkPath(root, path) {
		return result, errors.New("unsafe MP3 path")
	}
	f, e := os.Open(path)
	if e != nil {
		return result, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || !before.Mode().IsRegular() {
		return result, errors.New("MP3 unavailable")
	}
	start := int64(0)
	end, e := mp3AudioEnd(f, before.Size())
	if e != nil {
		return result, e
	}
	for start+10 <= end {
		var h [10]byte
		if _, e = f.ReadAt(h[:], start); e != nil {
			return result, e
		}
		if string(h[:3]) != "ID3" {
			break
		}
		if h[3] < 2 || h[3] > 4 {
			return result, errors.New("unsupported ID3 version")
		}
		size := int64(0)
		for _, b := range h[6:10] {
			if b&128 != 0 {
				return result, errors.New("malformed ID3 size")
			}
			size = (size << 7) | int64(b)
		}
		start += 10 + size
		if h[3] == 4 && h[5]&16 != 0 {
			start += 10
		}
		if start > end || start > 64<<20 {
			return result, errors.New("ID3 tag size exceeds bounds")
		}
	}
	decoder := mp3.NewDecoder(cancelAudioReader{ctx, io.NewSectionReader(f, start, end-start)})
	var frame mp3.Frame
	var frameBytes int64
	for {
		var skipped int
		e := decoder.Decode(&frame, &skipped)
		if e == io.EOF {
			break
		}
		if e != nil {
			return MP3Properties{}, e
		}
		if skipped != 0 {
			return MP3Properties{}, errors.New("discontinuous MPEG stream")
		}
		if frame.Header().BitRate() <= 0 || frame.Duration() <= 0 || frame.Size() <= 0 {
			return MP3Properties{}, errors.New("invalid MPEG frame")
		}
		result.Duration += frame.Duration()
		result.Frames++
		frameBytes += int64(frame.Size())
	}
	if result.Frames < 2 || result.Duration <= 0 {
		return MP3Properties{}, errors.New("no complete MP3 stream")
	}
	after, e := f.Stat()
	if e != nil {
		return MP3Properties{}, e
	}
	current, e := os.Stat(path)
	if e != nil || !noSymlinkPath(root, path) || !os.SameFile(before, current) || before.Size() != after.Size() || after.Size() != current.Size() || !before.ModTime().Equal(after.ModTime()) || !after.ModTime().Equal(current.ModTime()) {
		return MP3Properties{}, errors.New("MP3 changed during measurement")
	}
	result.DurationSeconds = int(math.Round(result.Duration.Seconds()))
	if result.DurationSeconds < 1 {
		result.DurationSeconds = 1
	}
	result.BitrateKbps = int(math.Round(float64(frameBytes) * 8 / result.Duration.Seconds() / 1000))
	return result, nil
}
