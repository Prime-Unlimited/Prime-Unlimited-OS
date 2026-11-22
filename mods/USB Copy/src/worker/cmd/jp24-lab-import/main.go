// Experimental JP24 fixed-volume rehearsals. Not part of the production runtime.
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

	"github.com/dhowden/tag"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return errors.New("explicit laboratory invocation required")
	}
	switch os.Args[1] {
	case "--authorized-usb-rehearsal":
		return runLibrary("/media/MP3 2", "/proc")
	case "--prepare-plain-source":
		return preparePlainSource()
	case "--authorized-plain-source-batch":
		return runBatch("/data/prime-unlimited/usb-copy-lab/plain-source", "/media/Denon", "/proc")
	case "--authorized-internal-batch":
		return runBatch("/media/MP3 2", "/media/Denon", "/proc")
	case "--authorized-internal-rehearsal":
		return runBetweenLibraries("/media/MP3 2", "/media/Denon", "/proc")
	default:
		return errors.New("explicit laboratory invocation required")
	}
}

func runLibrary(volume, procRoot string) error {
	return runBetweenLibraries(volume, volume, procRoot)
}

func runBetweenLibraries(sourceVolume, destinationVolume, procRoot string) error {
	sourceRoot := filepath.Join(sourceVolume, "Engine Library")
	root := filepath.Join(destinationVolume, "Engine Library")
	sourceReal, sourceErr := filepath.EvalSymlinks(sourceRoot)
	if sourceErr != nil || sourceReal != sourceRoot {
		return errors.New("source library unavailable or indirect")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil || real != root {
		return errors.New("test library unavailable or indirect")
	}
	path := filepath.Join(root, "Database2/m.db")
	if err := requireOffline(procRoot, path); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rw")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return err
	}
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 4194305 {
		return errors.New("unsupported laboratory schema")
	}
	sourceDB, err := sql.Open("sqlite", "file:"+filepath.Join(sourceRoot, "Database2/m.db")+"?mode=ro")
	if err != nil {
		return err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)
	if _, err = sourceDB.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return err
	}
	var sourceVersion int
	if err = sourceDB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&sourceVersion); err != nil || sourceVersion != version {
		return errors.New("source schema mismatch")
	}
	var rel string
	var length, bitrate int
	if err = sourceDB.QueryRowContext(ctx, "SELECT path,length,bitrate FROM Track WHERE path LIKE 'Music/%' AND lower(fileType)='mp3' AND length>0 ORDER BY id LIMIT 1").Scan(&rel, &length, &bitrate); err != nil {
		return err
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return errors.New("unsafe source")
	}
	source := filepath.Join(sourceRoot, clean)
	resolved, e := filepath.EvalSymlinks(source)
	if e != nil || resolved != source {
		return errors.New("indirect audio source")
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	metadata, err := tag.ReadFrom(f)
	if err != nil {
		return err
	}
	if _, err = f.Seek(0, 0); err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, "SELECT id,path FROM Track ORDER BY id")
	if err != nil {
		return err
	}
	existing := []usbcopy.ExistingTrackFile{}
	for rows.Next() {
		var file usbcopy.ExistingTrackFile
		if err = rows.Scan(&file.TrackID, &file.RelativePath); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, file)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	decisions, err := usbcopy.FindDuplicateAudio(ctx, sourceRoot, root, []string{rel}, existing)
	if err != nil {
		return err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	label := "USB Copy Device Test "
	if sourceVolume != destinationVolume {
		label = "USB Copy Internal Test "
	}
	backup := filepath.Join(root, "Database2", "usb-copy-before-"+stamp+".sqlite-backup")
	if _, err = os.Lstat(backup); !os.IsNotExist(err) {
		return errors.New("backup collision")
	}
	if _, err = db.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	if decisions[0].ExistingTrackID > 0 {
		reused := decisions[0].ExistingTrackID
		var targetRelative string
		if err = db.QueryRowContext(ctx, "SELECT path FROM Track WHERE id=?", reused).Scan(&targetRelative); err != nil {
			return err
		}
		check, err := usbcopy.FingerprintAudioFile(ctx, root, targetRelative)
		if err != nil {
			return err
		}
		if check.SHA256 != decisions[0].Fingerprint.SHA256 || check.Bytes != decisions[0].Fingerprint.Bytes {
			return errors.New("duplicate changed before reuse")
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = usbcopy.CreateOfflineEnginePlaylist(ctx, tx, label+stamp, []int64{reused}); err != nil {
			return err
		}
		var integrity string
		if err = tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
			return errors.New("candidate integrity failed")
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		fmt.Println("EXISTING_TRACK_REUSED; audio files copied: 0")
		fmt.Println("IMPORT_COMMITTED; playlist: " + label + stamp)
		return nil
	}
	folder := filepath.Join(root, "Music", label+stamp)
	if err = os.Mkdir(folder, 0700); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(folder)
		}
	}()
	audio := filepath.Join(folder, "Imported test.mp3")
	out, err := os.OpenFile(audio, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	stat, err := os.Stat(audio)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Artwork IDs are destination-local. Derive identity from embedded bytes.
	var art sql.NullInt64
	if picture := metadata.Picture(); picture != nil {
		prepared, e := usbcopy.PrepareEngineArtwork(picture.Data)
		if e != nil {
			return e
		}
		artPath := filepath.Join(root, "Artwork", prepared.Filename)
		existing, e := os.ReadFile(artPath)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if os.IsNotExist(e) {
			out, e := os.OpenFile(artPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, e = out.Write(prepared.JPEG)
			if e == nil {
				e = out.Sync()
			}
			ce := out.Close()
			defer func() {
				if !committed {
					os.Remove(artPath)
				}
			}()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
		} else if len(existing) == 0 {
			return errors.New("existing target artwork is empty")
		}
		var id int64
		e = tx.QueryRowContext(ctx, "SELECT id FROM AlbumArt WHERE hash=? ORDER BY id LIMIT 1", prepared.Hash).Scan(&id)
		if e == sql.ErrNoRows {
			result, e := tx.ExecContext(ctx, "INSERT INTO AlbumArt(hash,albumArt) VALUES(?,NULL)", prepared.Hash)
			if e != nil {
				return e
			}
			id, e = result.LastInsertId()
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		art = sql.NullInt64{Int64: id, Valid: true}

	}

	relative, _ := filepath.Rel(root, audio)
	result, err := tx.ExecContext(ctx, `INSERT INTO Track(path,filename,fileBytes,title,artist,album,genre,comment,composer,year,albumArtId,length,bitrate,fileType,isAnalyzed,isAvailable,isMetadataImported,isPlayed,isBeatGridLocked,dateCreated,dateAdded)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'mp3',0,1,1,0,0,strftime('%s'),strftime('%s'))`, filepath.ToSlash(relative), filepath.Base(audio), stat.Size(), metadata.Title(), metadata.Artist(), metadata.Album(), metadata.Genre(), metadata.Comment(), metadata.Composer(), metadata.Year(), art, length, bitrate)
	if err != nil {
		return err
	}
	tid, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err = usbcopy.CreateOfflineEnginePlaylist(ctx, tx, label+stamp, []int64{tid}); err != nil {
		return err
	}

	var n int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM PerformanceData WHERE trackId=? AND trackData IS NULL AND beatData IS NULL AND overviewWaveFormData IS NULL", tid).Scan(&n); err != nil || n != 1 {
		return errors.New("unexpected performance initialization")
	}
	var integrity string
	if err = tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("candidate integrity failed")
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	committed = true
	fmt.Println("IMPORT_COMMITTED; playlist: " + label + stamp)
	fmt.Println("DATABASE_BACKUP_CREATED; no analysis values copied")
	return nil
}

func requireOffline(procRoot, path string) error {
	return usbcopy.RequireOfflineLibrary(procRoot, path)
}
