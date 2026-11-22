package usbcopy

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"path/filepath"
)

type DuplicateSummary struct {
	SourceDigest     string `json:"sourceDigest"`
	NewFiles         int    `json:"newFiles"`
	ExistingFiles    int    `json:"existingFiles"`
	SourceDuplicates int    `json:"sourceDuplicates"`
	BytesToCopy      int64  `json:"bytesToCopy"`
}

// SummarizeImportDuplicates is read-only. Its byte estimate excludes indexed
// duplicates and repeated source content; unindexed destination files are not
// yet included. A writer must repeat content validation before committing.
func SummarizeImportDuplicates(ctx context.Context, sourceRoot, destinationRoot, databasePath string, tracks []Track) (DuplicateSummary, error) {
	return SummarizeImportDuplicatesWithProgress(ctx, sourceRoot, destinationRoot, databasePath, tracks, nil)
}

func SummarizeImportDuplicatesWithProgress(ctx context.Context, sourceRoot, destinationRoot, databasePath string, tracks []Track, progress DuplicateProgress) (DuplicateSummary, error) {
	var summary DuplicateSummary
	uri := (&url.URL{Scheme: "file", Path: databasePath, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return summary, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return summary, err
	}
	rows, err := db.QueryContext(ctx, "SELECT id,path FROM Track ORDER BY id")
	if err != nil {
		return summary, err
	}
	existing := []ExistingTrackFile{}
	for rows.Next() {
		var id int64
		var path sql.NullString
		if err = rows.Scan(&id, &path); err != nil {
			rows.Close()
			return summary, err
		}
		if !path.Valid {
			continue
		}
		normalized, ok := normalizeEngineTrackPath(path.String)
		if !ok {
			rows.Close()
			return summary, errors.New("unsafe destination library path")
		}
		existing = append(existing, ExistingTrackFile{TrackID: id, RelativePath: normalized})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return summary, err
	}
	sources := make([]string, len(tracks))
	for i, track := range tracks {
		sources[i] = filepath.ToSlash(track.RelativePath)
	}
	decisions, err := FindDuplicateAudioWithProgress(ctx, sourceRoot, destinationRoot, sources, existing, progress)
	if err != nil {
		return summary, err
	}
	for i, decision := range decisions {
		if decision.Fingerprint.Bytes != tracks[i].Bytes || (tracks[i].ModifiedAt != 0 && decision.Fingerprint.ModifiedAt != tracks[i].ModifiedAt) {
			return DuplicateSummary{}, errors.New("source changed after scan")
		}
		switch {
		case decision.ExistingTrackID > 0:
			summary.ExistingFiles++
		case decision.DuplicateOfSource != "":
			summary.SourceDuplicates++
		default:
			if decision.Fingerprint.Bytes < 0 || summary.BytesToCopy > math.MaxInt64-decision.Fingerprint.Bytes {
				return DuplicateSummary{}, errors.New("import size overflow")
			}
			summary.NewFiles++
			summary.BytesToCopy += decision.Fingerprint.Bytes
		}
	}
	summary.SourceDigest = DuplicateSourceDigest(decisions)
	return summary, nil
}
