package usbcopy

import (
	"context"
	"database/sql"
	"testing"
)

func writerFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, e = db.Exec(`CREATE TABLE Track(id INTEGER PRIMARY KEY,originDatabaseUuid TEXT,originTrackId INTEGER);
 INSERT INTO Track VALUES(1,'desktop',101),(2,'desktop',102),(3,'other',303);
 CREATE TABLE Playlist(id INTEGER PRIMARY KEY AUTOINCREMENT,title TEXT,parentListId INTEGER,isPersisted INTEGER,nextListId INTEGER,lastEditTime INTEGER,isExplicitlyExported INTEGER,UNIQUE(title,parentListId),UNIQUE(parentListId,nextListId));
 CREATE TABLE PlaylistEntity(id INTEGER PRIMARY KEY AUTOINCREMENT,listId INTEGER,trackId INTEGER,databaseUuid TEXT,nextEntityId INTEGER,membershipReference INTEGER,UNIQUE(listId,databaseUuid,trackId));
 CREATE TRIGGER before_list BEFORE INSERT ON Playlist BEGIN UPDATE Playlist SET nextListId=-(1+nextListId) WHERE nextListId=NEW.nextListId AND parentListId=NEW.parentListId;END;
 CREATE TRIGGER after_list AFTER INSERT ON Playlist BEGIN UPDATE Playlist SET nextListId=NEW.id WHERE nextListId=-(1+NEW.nextListId) AND parentListId=NEW.parentListId;END;`)
	if e != nil {
		t.Fatal(e)
	}
	return db
}
func TestBatchPlaylistPreservesOrderAndOrigins(t *testing.T) {
	db := writerFixture(t)
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	pid, e := CreateOfflineEnginePlaylist(context.Background(), tx, "Imported", []int64{3, 1, 2})
	if e != nil {
		t.Fatal(e)
	}
	rows, e := tx.Query("SELECT id,listId,trackId,databaseUuid,nextEntityId FROM PlaylistEntity WHERE listId=?", pid)
	if e != nil {
		t.Fatal(e)
	}
	entities := []engineEntityRow{}
	for rows.Next() {
		var row engineEntityRow
		if e := rows.Scan(&row.id, &row.listID, &row.trackID, &row.databaseUUID, &row.nextID); e != nil {
			t.Fatal(e)
		}
		entities = append(entities, row)
	}
	rows.Close()
	ordered := orderEngineEntities(entities)
	want := []int64{303, 101, 102}
	if len(ordered) != len(want) {
		t.Fatal("wrong membership count")
	}
	for i, row := range ordered {
		if row.trackID != want[i] {
			t.Fatal("wrong member order")
		}
		expected := "desktop"
		if i == 0 {
			expected = "other"
		}
		if row.databaseUUID.String != expected {
			t.Fatal("wrong origin database")
		}
	}
}
func TestBatchPlaylistRejectsInvalidMembersBeforeInsert(t *testing.T) {
	for _, ids := range [][]int64{nil, {1, 1}, {999}, {0}} {
		t.Run("invalid", func(t *testing.T) {
			db := writerFixture(t)
			tx, e := db.Begin()
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			if _, e := CreateOfflineEnginePlaylist(context.Background(), tx, "Imported", ids); e == nil {
				t.Fatal("invalid members accepted")
			}
			var n int
			if e := tx.QueryRow("SELECT count(*) FROM Playlist").Scan(&n); e != nil || n != 0 {
				t.Fatal("playlist created on rejected input")
			}
		})
	}
}
func TestBatchPlaylistNameConflictPreservesExistingList(t *testing.T) {
	db := writerFixture(t)
	ctx := context.Background()
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = CreateOfflineEnginePlaylist(ctx, tx, "Existing", []int64{1}); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	tx, e = db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = CreateOfflineEnginePlaylist(ctx, tx, "Existing", []int64{2}); e == nil {
		t.Fatal("name conflict accepted")
	}
	tx.Rollback()
	var track int
	if e = db.QueryRow("SELECT trackId FROM PlaylistEntity").Scan(&track); e != nil || track != 101 {
		t.Fatal("existing list changed")
	}
}
