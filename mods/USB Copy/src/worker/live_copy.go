package usbcopy

import (
	"context"
	"errors"
	"path/filepath"
)

// CopyLivePlan publishes only the new files, then verifies their actual bytes
// before returning them to the native registration adapter. A failure after
// publication deliberately leaves the import folder for review, never deleting
// files which a native operation might already have observed.
func CopyLivePlan(ctx context.Context, sourceRoot, destinationMusicRoot, jobID string, plan ImportPlan, decisions []DuplicateDecision, progress func(CopyProgress)) (CopyResult, error) {
	var result CopyResult
	filtered, skipped, err := LiveNewOnlyPlan(plan, decisions)
	if err != nil {
		return result, err
	}
	if skipped != 0 || len(filtered.Tracks) == 0 {
		return result, errors.New("live copy requires an already filtered new-only plan")
	}
	bySource := map[string]FileFingerprint{}
	for _, d := range decisions {
		bySource[d.SourceRelative] = d.Fingerprint
	}
	for _, t := range filtered.Tracks {
		actual, e := FingerprintAudioFile(ctx, sourceRoot, t.SourceRelative)
		if e != nil || actual != bySource[t.SourceRelative] {
			return result, errors.New("source changed before live copy")
		}
	}
	result, err = CopyImportPlan(sourceRoot, destinationMusicRoot, jobID, filtered, ctx.Done(), progress)
	if err != nil {
		return result, err
	}
	for _, t := range filtered.Tracks {
		actual, e := FingerprintAudioFile(ctx, destinationMusicRoot, filepath.ToSlash(t.DestinationRelative))
		expected := bySource[t.SourceRelative]
		if e != nil || actual.Bytes != expected.Bytes || actual.SHA256 != expected.SHA256 {
			return result, errors.New("published live copy failed content verification; registration blocked")
		}
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
