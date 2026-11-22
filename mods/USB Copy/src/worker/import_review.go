package usbcopy

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ImportReview describes visible artifacts only. It never certifies a commit,
// changes a database, removes files, or permits automatic retry.
type ImportReview struct {
	ReadOnly          bool   `json:"readOnly"`
	BackupAvailable   bool   `json:"backupAvailable"`
	ImportFolders     int    `json:"importFolders"`
	FilesOnDisk       int    `json:"filesOnDisk"`
	RegisteredFiles   int    `json:"registeredFiles"`
	MissingFiles      int    `json:"missingFiles"`
	UnregisteredFiles int    `json:"unregisteredFiles"`
	Playlists         int    `json:"playlists"`
	PlaylistMembers   int    `json:"playlistMembers"`
	MissingReferences int    `json:"missingReferences"`
	Assessment        string `json:"assessment"`
}

func ReviewImportArtifacts(ctx context.Context, destination, id string) (out ImportReview, err error) {
	out.ReadOnly = true
	out.Assessment = "manual_review_required"
	if !validJobStatusID(id) {
		return out, errors.New("invalid import job id")
	}
	real, resolveErr := filepath.EvalSymlinks(destination)
	if resolveErr != nil || real != destination || !filepath.IsAbs(destination) {
		return out, errors.New("destination unavailable or indirect")
	}
	root := filepath.Join(destination, "Engine Library")
	music := filepath.Join(root, "Music")
	database := filepath.Join(root, "Database2/m.db")
	if !noSymlinkPath(destination, music) || !noSymlinkPath(destination, database) {
		return out, errors.New("unsafe library paths")
	}
	backup := filepath.Join(root, "Database2", "usb-copy-before-batch-"+id+".sqlite-backup")
	if st, e := os.Lstat(backup); e == nil {
		if !st.Mode().IsRegular() || !noSymlinkPath(destination, backup) {
			return out, errors.New("unsafe backup")
		}
		out.BackupAvailable = st.Size() > 0
	} else if !os.IsNotExist(e) {
		return out, e
	}
	entries, err := os.ReadDir(music)
	if err != nil {
		return out, err
	}
	dirs := map[string]bool{}
	files := map[string]bool{}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "USB Copy - ") || !strings.HasSuffix(entry.Name(), " - "+id[:16]) {
			continue
		}
		if !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return out, errors.New("unsafe import directory")
		}
		folder := filepath.Join(music, entry.Name())
		dirs["Music/"+entry.Name()+"/"] = true
		out.ImportFolders++
		err = filepath.WalkDir(folder, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if e = ctx.Err(); e != nil {
				return e
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return errors.New("symlink in import artifacts")
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return errors.New("non-regular import artifact")
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			files[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	out.FilesOnDisk = len(files)
	uri := (&url.URL{Scheme: "file", Path: database, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return out, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return out, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT path FROM Track")
	if err != nil {
		return out, err
	}
	registered := map[string]bool{}
	for rows.Next() {
		var p sql.NullString
		if err = rows.Scan(&p); err != nil {
			rows.Close()
			return out, err
		}
		if !p.Valid {
			continue
		}
		rel, ok := normalizeEngineTrackPath(p.String)
		if !ok {
			continue
		}
		rel = strings.TrimPrefix(rel, "Engine Library/")
		belongs := false
		for prefix := range dirs {
			if strings.HasPrefix(rel, prefix) {
				belongs = true
				break
			}
		}
		// A removed entire import directory is still discoverable via its DB paths.
		parts := strings.Split(rel, "/")
		if len(parts) >= 3 && parts[0] == "Music" && strings.HasPrefix(parts[1], "USB Copy - ") && strings.HasSuffix(parts[1], " - "+id[:16]) {
			belongs = true
		}
		if belongs {
			registered[rel] = true
			out.RegisteredFiles++
			if !files[rel] {
				out.MissingFiles++
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for path := range files {
		if !registered[path] {
			out.UnregisteredFiles++
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT p.title,COUNT(e.id),SUM(CASE WHEN e.id IS NOT NULL AND t.id IS NULL THEN 1 ELSE 0 END) FROM Playlist p LEFT JOIN PlaylistEntity e ON e.listId=p.id LEFT JOIN Track t ON t.originDatabaseUuid=e.databaseUuid AND t.originTrackId=e.trackId WHERE instr(p.title,?)>0 GROUP BY p.id`, " - "+id[:16])
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var title string
		var count, missing int
		if err = rows.Scan(&title, &count, &missing); err != nil {
			rows.Close()
			return out, err
		}
		if !strings.HasPrefix(title, "USB Copy - ") {
			continue
		}
		out.Playlists++
		out.PlaylistMembers += count
		out.MissingReferences += missing
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if out.MissingFiles > 0 || out.UnregisteredFiles > 0 || out.MissingReferences > 0 {
		out.Assessment = "inconsistent_artifacts"
	} else if out.FilesOnDisk == 0 && out.RegisteredFiles == 0 && out.Playlists == 0 {
		out.Assessment = "no_visible_import_artifacts"
	} else {
		out.Assessment = "library_entries_present"
	}
	return out, nil
}
