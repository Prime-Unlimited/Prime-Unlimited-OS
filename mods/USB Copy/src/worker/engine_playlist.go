package usbcopy

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

type engineListRow struct {
	id, parentID int64
	title        string
}

type engineEntityRow struct {
	id, listID, trackID int64
	databaseUUID        sql.NullString
	nextID              sql.NullInt64
}

// readEnginePlaylists reads playlist metadata from Engine's m.db using a
// SQLite read-only URI and query_only. It never writes to the source library.
func readEnginePlaylists(databasePath string, trackByPath map[string]int) ([]Playlist, error) {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(absolute); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Engine database is unavailable")
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, errors.New("Engine database could not be opened read-only")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		return nil, errors.New("Engine database read-only mode could not be confirmed")
	}
	if err := db.Ping(); err != nil {
		return nil, errors.New("Engine database could not be read")
	}
	databaseUUID, err := readEngineDatabaseUUID(db)
	if err != nil {
		return nil, err
	}

	lists, err := readEngineLists(db)
	if err != nil {
		return nil, err
	}
	entities, err := readEngineEntities(db)
	if err != nil {
		return nil, err
	}
	tracks, err := readEngineTrackIdentities(db, databaseUUID)
	if err != nil {
		return nil, err
	}

	entitiesByList := make(map[int64][]engineEntityRow)
	for _, entity := range entities {
		entitiesByList[entity.listID] = append(entitiesByList[entity.listID], entity)
	}
	listNameByID := make(map[int64]string, len(lists))
	listByID := make(map[int64]engineListRow, len(lists))
	for _, list := range lists {
		listNameByID[list.id] = cleanTag(list.title)
		listByID[list.id] = list
	}

	result := make([]Playlist, 0, len(lists))
	for _, list := range lists {
		if strings.TrimSpace(list.title) == "" {
			continue
		}
		members := orderEngineEntities(entitiesByList[list.id])
		playlist := Playlist{Name: engineListDisplayName(list, listByID, listNameByID), RelativePath: "Engine Library/Database2/m.db", Tracks: make([]string, 0, len(members))}
		for _, entity := range members {
			uuid := databaseUUID
			if entity.databaseUUID.Valid && entity.databaseUUID.String != "" {
				uuid = entity.databaseUUID.String
			}
			sourcePath, ok := tracks[engineTrackIdentity{uuid, entity.trackID}]
			if !ok {
				playlist.Missing++
				continue
			}
			if _, ok := trackByPath[filepath.Clean(filepath.FromSlash(sourcePath))]; ok {
				playlist.Tracks = append(playlist.Tracks, sourcePath)
			} else {
				playlist.Missing++
			}
		}
		if len(playlist.Tracks) > 0 {
			result = append(result, playlist)
		}
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

// ReadEnginePlaylistNames reads the destination playlist names without
// exposing any track metadata. It opens the Engine database read-only.
func ReadEnginePlaylistNames(databasePath string) ([]string, error) {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(absolute); err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Engine database is unavailable")
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, errors.New("Engine database could not be opened read-only")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		return nil, errors.New("Engine database read-only mode could not be confirmed")
	}
	rows, err := db.Query("SELECT COALESCE(title, '') FROM Playlist")
	if err != nil {
		return nil, errors.New("Engine playlist names could not be read")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, errors.New("Engine playlist data is malformed")
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("Engine playlist names could not be read")
	}
	return names, nil
}

func readEngineDatabaseUUID(db *sql.DB) (string, error) {
	var uuid sql.NullString
	if err := db.QueryRow("SELECT uuid FROM Information ORDER BY id LIMIT 1").Scan(&uuid); err != nil || !uuid.Valid || strings.TrimSpace(uuid.String) == "" {
		return "", errors.New("Engine library identity is unavailable")
	}
	return uuid.String, nil
}

func readEngineLists(db *sql.DB) ([]engineListRow, error) {
	rows, err := db.Query("SELECT id, COALESCE(parentListId, 0), COALESCE(title, '') FROM Playlist ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read Engine playlists: %w", err)
	}
	defer rows.Close()
	var result []engineListRow
	for rows.Next() {
		var row engineListRow
		if err := rows.Scan(&row.id, &row.parentID, &row.title); err != nil {
			return nil, errors.New("Engine playlist data is malformed")
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("Engine playlist data could not be read")
	}
	return result, nil
}

func readEngineEntities(db *sql.DB) ([]engineEntityRow, error) {
	rows, err := db.Query("SELECT id, listId, trackId, databaseUuid, nextEntityId FROM PlaylistEntity ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read Engine playlist entries: %w", err)
	}
	defer rows.Close()
	var result []engineEntityRow
	for rows.Next() {
		var row engineEntityRow
		if err := rows.Scan(&row.id, &row.listID, &row.trackID, &row.databaseUUID, &row.nextID); err != nil {
			return nil, errors.New("Engine playlist entries are malformed")
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("Engine playlist entries could not be read")
	}
	return result, nil
}

func readEngineTrackPaths(db *sql.DB) (map[int64]string, error) {
	rows, err := db.Query("SELECT id, path FROM Track ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("read Engine track paths: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]string)
	for rows.Next() {
		var id int64
		var rawPath sql.NullString
		if err := rows.Scan(&id, &rawPath); err != nil {
			return nil, errors.New("Engine track data is malformed")
		}
		if !rawPath.Valid {
			continue
		}
		path, ok := normalizeEngineTrackPath(rawPath.String)
		if ok {
			result[id] = path
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("Engine track paths could not be read")
	}
	return result, nil
}

func normalizeEngineTrackPath(value string) (string, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimLeft(value, "/")
	if value == "" || strings.Contains(value, ":") {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if !safeRelative(clean) {
		return "", false
	}
	// Database2 Track.path is relative to the Engine Library directory.
	return filepath.ToSlash(filepath.Join("Engine Library", clean)), true
}

func orderEngineEntities(entities []engineEntityRow) []engineEntityRow {
	byID := make(map[int64]engineEntityRow, len(entities))
	pointedTo := make(map[int64]bool, len(entities))
	for _, entity := range entities {
		byID[entity.id] = entity
	}
	for _, entity := range entities {
		if entity.nextID.Valid {
			if _, ok := byID[entity.nextID.Int64]; ok {
				pointedTo[entity.nextID.Int64] = true
			}
		}
	}
	heads := make([]int64, 0)
	for _, entity := range entities {
		if !pointedTo[entity.id] {
			heads = append(heads, entity.id)
		}
	}
	sort.Slice(heads, func(i, j int) bool { return heads[i] < heads[j] })
	result := make([]engineEntityRow, 0, len(entities))
	seen := make(map[int64]bool, len(entities))
	appendChain := func(start int64) {
		for id := start; ; {
			entity, ok := byID[id]
			if !ok || seen[id] {
				return
			}
			seen[id] = true
			result = append(result, entity)
			if !entity.nextID.Valid {
				return
			}
			id = entity.nextID.Int64
		}
	}
	for _, head := range heads {
		appendChain(head)
	}
	for _, entity := range entities {
		if !seen[entity.id] {
			appendChain(entity.id)
		}
	}
	return result
}

func engineListDisplayName(list engineListRow, lists map[int64]engineListRow, names map[int64]string) string {
	parts := []string{cleanTag(list.title)}
	seen := map[int64]bool{list.id: true}
	parent := list.parentID
	for parent != 0 && !seen[parent] {
		seen[parent] = true
		name := names[parent]
		if name != "" {
			parts = append([]string{name}, parts...)
		}
		candidate, found := lists[parent]
		if !found {
			break
		}
		parent = candidate.parentID
	}
	return strings.Join(parts, " / ")
}

// Engine Desktop exports may retain playlist references to a track's origin
// identity instead of the destination library's local numeric ID.
type engineTrackIdentity struct {
	uuid string
	id   int64
}

func readEngineTrackIdentities(db *sql.DB, databaseUUID string) (map[engineTrackIdentity]string, error) {
	rows, err := db.Query("SELECT id, path, originDatabaseUuid, originTrackId FROM Track ORDER BY id")
	if err != nil {
		return nil, errors.New("Engine track identities could not be read")
	}
	defer rows.Close()
	result := make(map[engineTrackIdentity]string)
	for rows.Next() {
		var id int64
		var rawPath, originUUID sql.NullString
		var originID sql.NullInt64
		if err := rows.Scan(&id, &rawPath, &originUUID, &originID); err != nil {
			return nil, errors.New("Engine track identity is malformed")
		}
		if !rawPath.Valid {
			continue
		}
		path, ok := normalizeEngineTrackPath(rawPath.String)
		if !ok {
			continue
		}
		identities := []engineTrackIdentity{{databaseUUID, id}}
		if originUUID.Valid && originUUID.String != "" && originID.Valid && originID.Int64 > 0 {
			identities = append(identities, engineTrackIdentity{originUUID.String, originID.Int64})
		}
		for _, identity := range identities {
			if existing, exists := result[identity]; exists && existing != path {
				return nil, errors.New("Engine track identity is ambiguous")
			}
			result[identity] = path
		}
	}
	if rows.Err() != nil {
		return nil, errors.New("Engine track identities could not be read")
	}
	return result, nil
}
