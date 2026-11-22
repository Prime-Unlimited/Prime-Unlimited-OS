package usbcopy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runBatch is a laboratory operation for MP3 sources with or without an Engine library.
// It is not exposed through the production runtime or arbitrary caller paths.

// OfflineBatchOptions are supplied by the trusted supervisor, never package QML.
// Progress errors abort before commit and roll back newly created artifacts.
var ErrCommitUncertain = errors.New("library commit outcome requires reconciliation")

type OfflineBatchOptions struct {
	ExpectedSourceDigest string
	JobID                string
	Progress             func(JobPhase, CopyProgress) error
}
type OfflineBatchResult struct {
	Copied, Reused, SourceDuplicates, Playlists int
	PlaylistName                                string
	BackupPath                                  string // private supervisor data; do not expose to a mod
}

// RunOfflineBatch requires Engine stopped and an externally exclusive supervisor.
// It does not stop/restart Engine or implement power-loss reconciliation itself.
func RunOfflineBatch(ctx context.Context, sourceVolume, destinationVolume, procRoot string, options OfflineBatchOptions) (outcome OfflineBatchResult, err error) {
	if sourceVolume == destinationVolume {
		return outcome, errors.New("batch requires distinct volumes")
	}
	for _, root := range []string{sourceVolume, destinationVolume} {
		real, e := filepath.EvalSymlinks(root)
		if e != nil || real != root {
			return outcome, errors.New("volume unavailable or indirect")
		}
	}
	targetRoot := filepath.Join(destinationVolume, "Engine Library")
	dbpath := filepath.Join(targetRoot, "Database2/m.db")
	if e := RequireOfflineLibrary(procRoot, dbpath); e != nil {
		return outcome, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	snapshot, e := Scan(sourceVolume)
	if e != nil {
		return outcome, withImportCode("source_scan_failed", e)
	}
	if len(snapshot.Tracks) == 0 {
		return outcome, errors.New("no source tracks")
	}
	if len(snapshot.Warnings) > 0 {
		return outcome, withImportCode("source_scan_failed", errors.New("source library scan has warnings; batch aborted"))
	}
	db, e := sql.Open("sqlite", "file:"+dbpath+"?mode=rw")
	if e != nil {
		return outcome, e
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); e != nil {
		return outcome, e
	}
	for _, d := range []*sql.DB{db} {
		var v int
		if e = d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); e != nil || v != 4194305 {
			return outcome, withImportCode("unsupported_library", errors.New("unsupported batch schema"))
		}
	}
	var state string
	if e = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&state); e != nil || state != "ok" {
		return outcome, withImportCode("library_integrity_failed", errors.New("destination integrity failed before batch"))
	}
	type duration struct{ seconds, bitrate int }
	durations := map[string]duration{}
	sources := []string{}
	for _, tr := range snapshot.Tracks {
		if strings.ToLower(filepath.Ext(tr.RelativePath)) != ".mp3" {
			return outcome, withImportCode("unsupported_audio", errors.New("laboratory batch currently supports MP3 audio only"))
		}
		properties, e := ReadMP3Properties(ctx, sourceVolume, tr.RelativePath)
		if e != nil {
			return outcome, withImportCode("invalid_mp3", e)
		}
		d := duration{seconds: properties.DurationSeconds, bitrate: properties.BitrateKbps}
		durations[tr.RelativePath] = d
		sources = append(sources, tr.RelativePath)
	}
	rows, e := db.QueryContext(ctx, "SELECT id,path FROM Track ORDER BY id")
	if e != nil {
		return outcome, e
	}
	existing := []ExistingTrackFile{}
	targetPaths := map[int64]string{}
	for rows.Next() {
		var id int64
		var path string
		if e = rows.Scan(&id, &path); e != nil {
			rows.Close()
			return outcome, e
		}
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			rows.Close()
			return outcome, errors.New("unsafe destination track path")
		}
		rel := filepath.ToSlash(filepath.Join("Engine Library", clean))
		existing = append(existing, ExistingTrackFile{TrackID: id, RelativePath: rel})
		targetPaths[id] = rel
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return outcome, e
	}
	decisions, e := FindDuplicateAudio(ctx, sourceVolume, destinationVolume, sources, existing)
	if e != nil {
		return outcome, e
	}
	if options.ExpectedSourceDigest != "" && DuplicateSourceDigest(decisions) != options.ExpectedSourceDigest {
		return outcome, withImportCode("source_changed", errors.New("source content differs from approved preview"))
	}
	dbInfo, e := os.Stat(dbpath)
	if e != nil {
		return outcome, e
	}
	working, e := batchWorkingBytes(decisions, dbInfo.Size())
	if e != nil {
		return outcome, e
	}
	if e = requireBatchSpace(destinationVolume, working); e != nil {
		return outcome, e
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	label := "USB Copy Batch Test " + stamp
	if options.JobID != "" {
		if !validJobStatusID(options.JobID) {
			return outcome, errors.New("invalid import job id")
		}
		label = "USB Copy - " + cleanImportLabel(filepath.Base(sourceVolume)) + " - " + time.Now().Format("2006-01-02") + " - " + options.JobID[:16]
	}
	backupToken := stamp
	if options.JobID != "" {
		backupToken = options.JobID
	}
	backup := filepath.Join(targetRoot, "Database2", "usb-copy-before-batch-"+backupToken+".sqlite-backup")
	if _, e = os.Stat(backup); !os.IsNotExist(e) {
		return outcome, errors.New("backup collision")
	}
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", backup); e != nil {
		return outcome, e
	}
	outcome.BackupPath = backup
	folder := filepath.Join(targetRoot, "Music", label)
	createdFolder := false
	artifacts := []string{}
	committed := false
	commitAttempted := false
	defer func() {
		if !committed && !commitAttempted {
			if createdFolder {
				os.RemoveAll(folder)
			}
			for _, path := range artifacts {
				os.Remove(path)
			}
		}
	}()
	progress := CopyProgress{FilesTotal: len(snapshot.Tracks)}
	for _, decision := range decisions {
		if decision.ExistingTrackID == 0 && decision.DuplicateOfSource == "" {
			progress.BytesTotal += decision.Fingerprint.Bytes
		}
	}
	report := func(phase JobPhase) error {
		if options.Progress != nil {
			return options.Progress(phase, progress)
		}
		return nil
	}
	if e = report(JobCopying); e != nil {
		return outcome, e
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return outcome, e
	}
	defer tx.Rollback()
	ids := map[string]int64{}
	newlyCopied := map[string]bool{}
	copied, reused, batchDuplicates := 0, 0, 0
	for i, tr := range snapshot.Tracks {
		decision := decisions[i]
		if decision.ExistingTrackID > 0 {
			fp, e := FingerprintAudioFile(ctx, destinationVolume, targetPaths[decision.ExistingTrackID])
			if e != nil {
				return outcome, e
			}
			if fp.SHA256 != decision.Fingerprint.SHA256 {
				return outcome, errors.New("existing track changed")
			}
			ids[tr.RelativePath] = decision.ExistingTrackID
			reused++
			progress.FilesDone = i + 1
			if e = report(JobCopying); e != nil {
				return outcome, e
			}
			continue
		}
		if decision.DuplicateOfSource != "" {
			id, ok := ids[decision.DuplicateOfSource]
			if !ok {
				return outcome, errors.New("unresolved batch duplicate")
			}
			ids[tr.RelativePath] = id
			batchDuplicates++
			progress.FilesDone = i + 1
			if e = report(JobCopying); e != nil {
				return outcome, e
			}
			continue
		}
		if !createdFolder {
			if e = os.Mkdir(folder, 0700); e != nil {
				return outcome, e
			}
			createdFolder = true
		}
		audio := filepath.Join(folder, fmt.Sprintf("%04d-%s", i+1, filepath.Base(tr.RelativePath)))
		if e = CopyLaboratoryAudio(ctx, filepath.Join(sourceVolume, filepath.FromSlash(tr.RelativePath)), audio); e != nil {
			return outcome, e
		}
		copiedRelative, _ := filepath.Rel(destinationVolume, audio)
		fp, e := FingerprintAudioFile(ctx, destinationVolume, filepath.ToSlash(copiedRelative))
		if e != nil {
			return outcome, e
		}
		if fp.SHA256 != decision.Fingerprint.SHA256 || fp.Bytes != decision.Fingerprint.Bytes {
			return outcome, errors.New("copied file differs from source plan")
		}
		var art sql.NullInt64
		if len(tr.Artwork) > 0 {
			prepared, e := PrepareEngineArtwork(tr.Artwork)
			if e != nil {
				return outcome, e
			}
			path := filepath.Join(targetRoot, "Artwork", prepared.Filename)
			if _, e = os.Stat(path); os.IsNotExist(e) {
				f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					return outcome, e
				}
				artifacts = append(artifacts, path)
				_, e = f.Write(prepared.JPEG)
				if e == nil {
					e = f.Sync()
				}
				ce := f.Close()
				if e != nil {
					return outcome, e
				}
				if ce != nil {
					return outcome, ce
				}
			} else if e != nil {
				return outcome, e
			}
			var aid int64
			e = tx.QueryRowContext(ctx, "SELECT id FROM AlbumArt WHERE hash=? ORDER BY id LIMIT 1", prepared.Hash).Scan(&aid)
			if e == sql.ErrNoRows {
				result, e := tx.ExecContext(ctx, "INSERT INTO AlbumArt(hash,albumArt) VALUES(?,NULL)", prepared.Hash)
				if e != nil {
					return outcome, e
				}
				aid, e = result.LastInsertId()
				if e != nil {
					return outcome, e
				}
			} else if e != nil {
				return outcome, e
			}
			art = sql.NullInt64{Int64: aid, Valid: true}
		}
		relative, _ := filepath.Rel(targetRoot, audio)
		d := durations[tr.RelativePath]
		tags := tr.Tags
		tags.Title = ImportTrackTitle(tags, tr.RelativePath)
		result, e := tx.ExecContext(ctx, `INSERT INTO Track(path,filename,fileBytes,title,artist,album,genre,comment,composer,year,albumArtId,length,bitrate,fileType,isAnalyzed,isAvailable,isMetadataImported,isPlayed,isBeatGridLocked,dateCreated,dateAdded) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'mp3',0,1,1,0,0,strftime('%s'),strftime('%s'))`, filepath.ToSlash(relative), filepath.Base(audio), fp.Bytes, tags.Title, tags.Artist, tags.Album, tags.Genre, tags.Comment, tags.Composer, tags.Year, art, d.seconds, d.bitrate)
		if e != nil {
			return outcome, e
		}
		id, e := result.LastInsertId()
		if e != nil {
			return outcome, e
		}
		ids[tr.RelativePath] = id
		newlyCopied[tr.RelativePath] = true
		copied++
		progress.FilesDone = i + 1
		progress.BytesDone += fp.Bytes
		if e = report(JobCopying); e != nil {
			return outcome, e
		}
	}
	uniqueMembers := func(paths []string) []int64 {
		out := []int64{}
		seen := map[int64]bool{}
		for _, p := range paths {
			if id, ok := ids[p]; ok && newlyCopied[p] && !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
		return out
	}
	all := uniqueMembers(sources)
	playlists := 0
	if len(all) > 0 {
		if _, e = CreateOfflineEnginePlaylist(ctx, tx, label, all); e != nil {
			return outcome, e
		}
		outcome.PlaylistName = label
		playlists++
	}

	for i, list := range snapshot.Playlists {
		members := uniqueMembers(list.Tracks)
		if len(members) == 0 {
			continue
		}
		title := fmt.Sprintf("%s - %d - %s", label, i+1, list.Name)
		if _, e = CreateOfflineEnginePlaylist(ctx, tx, title, members); e != nil {
			return outcome, e
		}
		playlists++
	}
	if e = tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&state); e != nil || state != "ok" {
		return outcome, errors.New("batch integrity failed")
	}
	if e = report(JobCommitting); e != nil {
		return outcome, e
	}
	commitAttempted = true
	if e = tx.Commit(); e != nil {
		return outcome, errors.Join(ErrCommitUncertain, e)
	}
	committed = true
	outcome.Copied = copied
	outcome.Reused = reused
	outcome.SourceDuplicates = batchDuplicates
	outcome.Playlists = playlists
	return outcome, nil
}

func CopyLaboratoryAudio(ctx context.Context, source, target string) error {
	src, e := os.Open(source)
	if e != nil {
		return e
	}
	defer src.Close()
	dst, e := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer dst.Close()
	buffer := make([]byte, 128*1024)
	for {
		if e = ctx.Err(); e != nil {
			return e
		}
		n, err := src.Read(buffer)
		if n > 0 {
			written, e := dst.Write(buffer[:n])
			if e != nil {
				return e
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return dst.Sync()
}
