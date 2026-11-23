package main

import (
	"context"
	"os"
	"testing"
)

func TestMediaLiveReviewDeviceReadOnly(t *testing.T) {
	if os.Getenv("PRIME_USB_LIVE_REVIEW") != "1" {
		t.Skip("explicit read-only review required")
	}
	db, e := openNativeReadDB("/media/Denon/Engine Library/Database2/m.db")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	counts, e := nativeCounts(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("library counts: %+v", counts)
	rows, e := db.Query("SELECT id,title,parentListId FROM Playlist WHERE title=?", "USB Copy - MP3 2 - 2026-11-23 - f86093c8943c85c1")
	if e != nil {
		t.Fatal(e)
	}
	type listRow struct {
		ID, Parent int64
		Title      string
	}
	lists := []listRow{}
	for rows.Next() {
		var item listRow
		if e = rows.Scan(&item.ID, &item.Title, &item.Parent); e != nil {
			rows.Close()
			t.Fatal(e)
		}
		lists = append(lists, item)
	}
	rows.Close()
	for _, item := range lists {
		var n int
		if e = db.QueryRow("SELECT count(*) FROM PlaylistEntity WHERE listId=?", item.ID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		t.Logf("playlist: id=%d parent=%d members=%d title=%s", item.ID, item.Parent, n, item.Title)
	}
	rows, e = db.Query("SELECT id,path,title,artist,fileBytes FROM Track WHERE path LIKE ?", "%f86093c8943c85c1%")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id, size int64
		var path, title, artist string
		if e = rows.Scan(&id, &path, &title, &artist, &size); e != nil {
			t.Fatal(e)
		}
		t.Logf("track: id=%d bytes=%d title=%s artist=%s path=%s", id, size, title, artist, path)
	}
	rows.Close()
}
