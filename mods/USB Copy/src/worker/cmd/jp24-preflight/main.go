// Read-only inspection for the USB-to-internal JP24 laboratory workflow.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type library struct {
	Role          string `json:"role"`
	DatabaseBytes int64  `json:"databaseBytes"`
	UserVersion   int    `json:"userVersion"`
	SchemaSHA256  string `json:"schemaSHA256"`
	Tracks        int    `json:"tracks"`
	Playlists     int    `json:"playlists"`
	Artwork       int    `json:"artwork"`
}

func inspect(role, root string) (library, error) {
	result := library{Role: role}
	path := filepath.Join(root, "Engine Library/Database2/m.db")
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return result, errors.New("library unavailable or indirect")
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return result, errors.New("database unavailable")
	}
	result.DatabaseBytes = st.Size()
	uri := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return result, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return result, err
	}
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout=500"); err != nil {
		return result, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.UserVersion); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type,name")
	if err != nil {
		return result, err
	}
	h := sha256.New()
	for rows.Next() {
		var typ, name, sqlText string
		if err = rows.Scan(&typ, &name, &sqlText); err != nil {
			rows.Close()
			return result, err
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", typ, name, sqlText)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	result.SchemaSHA256 = hex.EncodeToString(h.Sum(nil))
	for _, item := range []struct {
		query string
		out   *int
	}{{"SELECT count(*) FROM Track", &result.Tracks}, {"SELECT count(*) FROM Playlist", &result.Playlists}, {"SELECT count(*) FROM AlbumArt", &result.Artwork}} {
		if err = tx.QueryRowContext(ctx, item.query).Scan(item.out); err != nil {
			return result, err
		}
	}
	return result, nil
}
func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--internal-integrity" || os.Args[1] == "--baseline-integrity") {
		dsn := "file:/media/Denon/Engine%20Library/Database2/m.db?mode=ro"
		if os.Args[1] == "--baseline-integrity" {
			dsn = "file:/data/prime-unlimited/backups/usb-copy/internal-20261121T081110Z/files/m.db?mode=ro"
		}
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			panic(err)
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
			panic(err)
		}
		rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
		if err != nil {
			panic(err)
		}
		defer rows.Close()
		messages := []string{}
		for rows.Next() {
			var v string
			if err = rows.Scan(&v); err != nil {
				panic(err)
			}
			messages = append(messages, v)
			if len(messages) >= 5 {
				break
			}
		}
		if err = rows.Err(); err != nil {
			panic(err)
		}
		json.NewEncoder(os.Stdout).Encode(messages)
		return
	}

	results := []library{}
	for _, v := range []struct{ role, root string }{{"USB source", "/media/MP3 2"}, {"internal SATA destination", "/media/Denon"}} {
		r, err := inspect(v.role, v.root)
		if err != nil {
			fmt.Fprintln(os.Stderr, v.role+": inspection failed: "+err.Error())
			os.Exit(1)
		}
		results = append(results, r)
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		os.Exit(1)
	}
}
