package usbcopy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type ExistingTrackFile struct {
	TrackID      int64
	RelativePath string
}
type FileFingerprint struct {
	SHA256     string
	Bytes      int64
	ModifiedAt int64
}
type DuplicateDecision struct {
	SourceRelative    string
	Fingerprint       FileFingerprint
	ExistingTrackID   int64  // Reuse this destination library entry when positive.
	DuplicateOfSource string // Otherwise reuse the first copy within this batch.
}

// FingerprintAudioFile hashes only regular, contained, non-symlink files and
// rejects changes observed while reading. Revalidate immediately before commit.
func FingerprintAudioFile(ctx context.Context, root, relative string) (FileFingerprint, error) {
	var result FileFingerprint
	rel, err := cleanRelative(relative)
	if err != nil {
		return result, err
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	if !noSymlinkPath(root, path) {
		return result, errors.New("unsafe audio path")
	}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return result, errors.New("audio is not a regular file")
	}
	h := sha256.New()
	buf := make([]byte, 128*1024)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		count, e := f.Read(buf)
		if count > 0 {
			h.Write(buf[:count])
			n += int64(count)
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return result, e
		}
	}
	after, err := f.Stat()
	if err != nil {
		return result, err
	}
	current, err := os.Stat(path)
	if err != nil || !noSymlinkPath(root, path) || !os.SameFile(before, current) || before.Size() != n || after.Size() != n || current.Size() != n || !before.ModTime().Equal(after.ModTime()) || !after.ModTime().Equal(current.ModTime()) {
		return result, errors.New("audio changed while hashing")
	}
	return FileFingerprint{hex.EncodeToString(h.Sum(nil)), n, after.ModTime().UnixNano()}, nil
}

// FindDuplicateAudio filters by actual file size before hashing destination
// candidates. Matches require byte-identical SHA-256, never filename equality.
// It reads media only; it neither copies files nor writes the library.
func FindDuplicateAudio(ctx context.Context, sourceRoot, destinationRoot string, sources []string, existing []ExistingTrackFile) ([]DuplicateDecision, error) {
	return FindDuplicateAudioWithProgress(ctx, sourceRoot, destinationRoot, sources, existing, nil)
}

// DuplicateProgress reports completed units without revealing track metadata.
type DuplicateProgress func(stage string, done, total int)

func FindDuplicateAudioWithProgress(ctx context.Context, sourceRoot, destinationRoot string, sources []string, existing []ExistingTrackFile, progress DuplicateProgress) ([]DuplicateDecision, error) {
	report := func(stage string, done, total int) {
		if progress != nil {
			progress(stage, done, total)
		}
	}
	fingerprints := make([]FileFingerprint, len(sources))
	wantedSizes := map[int64]bool{}
	report("source", 0, len(sources))
	for i, source := range sources {
		fp, err := FingerprintAudioFile(ctx, sourceRoot, source)
		if err != nil {
			return nil, err
		}
		fingerprints[i] = fp
		wantedSizes[fp.Bytes] = true
		report("source", i+1, len(sources))
	}
	type candidate struct {
		file ExistingTrackFile
		size int64
	}
	bySize := map[int64][]candidate{}
	sorted := append([]ExistingTrackFile(nil), existing...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TrackID < sorted[j].TrackID })
	report("index", 0, len(sorted))
	for i, file := range sorted {
		if i%128 == 0 {
			report("index", i, len(sorted))
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.TrackID <= 0 {
			return nil, errors.New("invalid existing track ID")
		}
		rel, err := cleanRelative(file.RelativePath)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(destinationRoot, filepath.FromSlash(rel))
		// Validate matching candidates again before opening any contents.
		st, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !st.Mode().IsRegular() || !wantedSizes[st.Size()] || !noSymlinkPath(destinationRoot, path) {
			continue
		}
		bySize[st.Size()] = append(bySize[st.Size()], candidate{file, st.Size()})
	}
	report("index", len(sorted), len(sorted))
	cache := map[string]FileFingerprint{}
	firstSource := map[string]string{}
	out := make([]DuplicateDecision, 0, len(sources))
	report("compare", 0, len(sources))
	for i, source := range sources {
		fp := fingerprints[i]
		var err error
		decision := DuplicateDecision{SourceRelative: source, Fingerprint: fp}
		for _, c := range bySize[fp.Bytes] {
			target, ok := cache[c.file.RelativePath]
			if !ok {
				target, err = FingerprintAudioFile(ctx, destinationRoot, c.file.RelativePath)
				if err != nil {
					return nil, err
				}
				cache[c.file.RelativePath] = target
			}
			if target.SHA256 == fp.SHA256 && target.Bytes == fp.Bytes {
				decision.ExistingTrackID = c.file.TrackID
				break
			}
		}
		if decision.ExistingTrackID == 0 {
			if first, ok := firstSource[fp.SHA256]; ok {
				decision.DuplicateOfSource = first
			} else {
				firstSource[fp.SHA256] = source
			}
		}
		out = append(out, decision)
		report("compare", i+1, len(sources))
	}
	return out, nil
}

// DuplicateSourceDigest binds approval to source paths, sizes and actual bytes.
func DuplicateSourceDigest(decisions []DuplicateDecision) string {
	h := sha256.New()
	for _, d := range decisions {
		b, _ := json.Marshal([]any{d.SourceRelative, d.Fingerprint.Bytes, d.Fingerprint.SHA256})
		h.Write(b)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
