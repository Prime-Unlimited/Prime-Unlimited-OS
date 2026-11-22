package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	usbcopy "prime-unlimited.usb-copy/worker"
	"strings"
	"time"
)

// Builds a disposable on-device fixture. The actual batch never opens an
// Engine database in this source folder. Original USB media is read only.
func preparePlainSource() error {
	const root = "/data/prime-unlimited/usb-copy-lab/plain-source"
	if _, e := os.Lstat(root); !os.IsNotExist(e) {
		return errors.New("plain-source fixture already exists")
	}
	db, e := sql.Open("sqlite", "file:/media/MP3%202/Engine%20Library/Database2/m.db?mode=ro")
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = db.Exec("PRAGMA query_only=ON"); e != nil {
		return e
	}
	rows, e := db.Query("SELECT path FROM Track WHERE lower(fileType)='mp3' ORDER BY id LIMIT 2")
	if e != nil {
		return e
	}
	paths := []string{}
	for rows.Next() {
		var path string
		if e = rows.Scan(&path); e != nil {
			rows.Close()
			return e
		}
		paths = append(paths, path)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(paths) != 2 {
		return errors.New("two fixture songs required")
	}
	if e = os.MkdirAll(filepath.Join(root, "Songs"), 0700); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stamp := time.Now().UTC().Format("20060102T150405Z")
	for i, path := range paths {
		clean := filepath.Clean(filepath.FromSlash(path))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return errors.New("unsafe fixture source")
		}
		source := filepath.Join("/media/MP3 2/Engine Library", clean)
		real, e := filepath.EvalSymlinks(source)
		if e != nil || real != source {
			return errors.New("indirect fixture source")
		}
		target := filepath.Join(root, "Songs", fmt.Sprintf("%d.mp3", i+1))
		if e = usbcopy.CopyLaboratoryAudio(ctx, source, target); e != nil {
			return e
		}
		// Replace or append a bounded ID3v1 test tag on the COPY. MPEG audio and
		// embedded ID3v2 artwork stay unchanged; the byte identity becomes new.
		f, e := os.OpenFile(target, os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		end := st.Size()
		if end >= 128 {
			var mark [3]byte
			if _, e = f.ReadAt(mark[:], end-128); e != nil {
				f.Close()
				return e
			}
			if string(mark[:]) == "TAG" {
				end -= 128
			}
		}
		tag := make([]byte, 128)
		copy(tag, "TAG")
		copy(tag[3:33], fmt.Sprintf("USB Copy Plain Test %d", i+1))
		copy(tag[33:63], "USB Copy Laboratory")
		copy(tag[63:93], "Plain source test")
		copy(tag[93:97], "2026")
		copy(tag[97:127], stamp)
		tag[127] = 255
		if _, e = f.Seek(end, io.SeekStart); e != nil {
			f.Close()
			return e
		}
		if _, e = f.Write(tag); e != nil {
			f.Close()
			return e
		}
		if e = f.Truncate(end + 128); e != nil {
			f.Close()
			return e
		}
		if e = f.Sync(); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
	}
	if e = os.WriteFile(filepath.Join(root, "Plain Source Test.m3u"), []byte("Songs/2.mp3\nSongs/1.mp3\n"), 0600); e != nil {
		return e
	}
	fmt.Println("PLAIN_SOURCE_READY; two MP3 copies, one M3U, no Engine Library")
	return nil
}
