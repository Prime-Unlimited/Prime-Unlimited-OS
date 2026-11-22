package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func nativeCreateFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{
		`CREATE TABLE Information(id INTEGER, uuid TEXT)`,
		`INSERT INTO Information VALUES(1,'test-uuid')`,
		`CREATE TABLE Track(id INTEGER)`,
		`INSERT INTO Track VALUES(1)`,
		`CREATE TABLE Playlist(id INTEGER,title TEXT,parentListId INTEGER,nextListId INTEGER)`,
		`CREATE TABLE PlaylistEntity(listId INTEGER)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return path, db
}
func TestNativeCreateSnapshotReadOnly(t *testing.T) {
	path, _ := nativeCreateFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "snapshot.db")
	counts, err := snapshotNativeCreate(context.Background(), path, backup, "test-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if counts != (nativeLibraryCounts{Tracks: 1}) {
		t.Fatalf("counts: %+v", counts)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("source database changed")
	}
	rejected := filepath.Join(t.TempDir(), "wrong.db")
	if _, err := snapshotNativeCreate(context.Background(), path, rejected, "wrong-uuid"); err == nil {
		t.Fatal("accepted wrong library")
	}
	if _, err := os.Stat(rejected); !os.IsNotExist(err) {
		t.Fatal("created backup for wrong library")
	}
}
func TestNativeCreateVerificationNeedsModelAndExactCounts(t *testing.T) {
	path, db := nativeCreateFixture(t)
	state := nativeCreateState{Title: "unique test", Phase: "submitted", Before: nativeLibraryCounts{Tracks: 1}}
	if _, err := db.Exec(`INSERT INTO Playlist VALUES(2,'unique test',0,0)`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeCreate(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase == "verified" {
		t.Fatal("verified without model signal")
	}
	state.RowsInserted = true
	if _, err := db.Exec(`INSERT INTO Track VALUES(3)`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeCreate(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase == "verified" {
		t.Fatal("verified concurrent track change")
	}
	if _, err := db.Exec(`DELETE FROM Track WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeCreate(context.Background(), &state, path); err != nil {
		t.Fatal(err)
	}
	if state.Phase != "verified" || state.PlaylistID != 2 {
		t.Fatalf("not verified: %+v", state)
	}
	state.Phase = "submitted"
	if _, err := db.Exec(`INSERT INTO PlaylistEntity VALUES(2)`); err != nil {
		t.Fatal(err)
	}
	if err := verifyNativeCreate(context.Background(), &state, path); err == nil {
		t.Fatal("accepted unexpected members")
	}
}
func TestNativeCreateLeaseExcludesOtherNativePreparation(t *testing.T) {
	root := t.TempDir()
	release, err := nativeCreateLease(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if second, err := nativeCreateLease(root); err == nil {
		second()
		t.Fatal("second lease accepted")
	}
}
