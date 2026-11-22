package usbcopy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportReviewReadsCommittedAndMissingArtifactsWithoutRepair(t *testing.T) {
	source, target, proc := batchFixture(t)
	id := strings.Repeat("6", 64)
	before, err := ReviewImportArtifacts(context.Background(), target, id)
	if err != nil || before.Assessment != "no_visible_import_artifacts" || before.BackupAvailable {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	result, err := RunOfflineBatch(context.Background(), source, target, proc, OfflineBatchOptions{JobID: id})
	if err != nil {
		t.Fatal(err)
	}
	review, err := ReviewImportArtifacts(context.Background(), target, id)
	if err != nil || !review.ReadOnly || !review.BackupAvailable || review.FilesOnDisk != 2 || review.RegisteredFiles != 2 || review.Playlists != 2 || review.PlaylistMembers != 4 || review.Assessment != "library_entries_present" {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	folder := filepath.Join(target, "Engine Library/Music", result.PlaylistName)
	files, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(folder, files[0].Name())
	if err = os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(folder, "orphan.mp3")
	if err = os.WriteFile(orphan, []byte("orphan"), 0600); err != nil {
		t.Fatal(err)
	}
	review, err = ReviewImportArtifacts(context.Background(), target, id)
	if err != nil || review.MissingFiles != 1 || review.UnregisteredFiles != 1 || review.Assessment != "inconsistent_artifacts" {
		t.Fatalf("review=%+v err=%v", review, err)
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("review restored missing audio")
	}
	if _, err = os.Stat(orphan); err != nil {
		t.Fatal("review removed orphan audio")
	}
	if err = os.Symlink(filepath.Join(source, "one.mp3"), filepath.Join(folder, "link.mp3")); err != nil {
		t.Fatal(err)
	}
	if _, err = ReviewImportArtifacts(context.Background(), target, id); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestImportReviewRejectsUnknownIdentityAndCancellation(t *testing.T) {
	_, target, _ := batchFixture(t)
	if _, err := ReviewImportArtifacts(context.Background(), target, "../unsafe"); err == nil {
		t.Fatal("invalid identity accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReviewImportArtifacts(ctx, target, strings.Repeat("7", 64)); err == nil {
		t.Fatal("cancellation ignored")
	}
}
