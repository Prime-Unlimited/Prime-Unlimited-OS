package usbcopy

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// CreateOfflineEnginePlaylist creates a root playlist for destination-local
// track IDs, preserving caller order. It resolves each member's origin identity
// from the destination itself, never from an untrusted source library.
// The caller must own exclusive offline library access and roll back tx on any
// error. This helper does not coordinate Engine or commit the transaction.
func CreateOfflineEnginePlaylist(ctx context.Context, tx *sql.Tx, title string, trackIDs []int64) (int64, error) {
	if tx == nil || strings.TrimSpace(title) == "" || len(trackIDs) == 0 {
		return 0, errors.New("playlist requires a title and imported tracks")
	}
	type reference struct {
		uuid string
		id   int64
	}
	refs := make([]reference, 0, len(trackIDs))
	seen := map[int64]bool{}
	for _, id := range trackIDs {
		if id <= 0 || seen[id] {
			return 0, errors.New("invalid or duplicate destination track")
		}
		seen[id] = true
		var uuid sql.NullString
		var origin sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT originDatabaseUuid,originTrackId FROM Track WHERE id=?", id).Scan(&uuid, &origin); err != nil {
			return 0, errors.New("destination track not found")
		}
		if !uuid.Valid || strings.TrimSpace(uuid.String) == "" || !origin.Valid || origin.Int64 <= 0 {
			return 0, errors.New("destination track identity missing")
		}
		refs = append(refs, reference{uuid.String, origin.Int64})
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO Playlist(title,parentListId,isPersisted,nextListId,lastEditTime,isExplicitlyExported) VALUES(?,0,1,0,strftime('%s'),1)", title)
	if err != nil {
		return 0, err
	}
	pid, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	// Insert backwards so nextEntityId forms the requested display order.
	var next int64
	for i := len(refs) - 1; i >= 0; i-- {
		result, err = tx.ExecContext(ctx, "INSERT INTO PlaylistEntity(listId,trackId,databaseUuid,nextEntityId,membershipReference) VALUES(?,?,?,?,0)", pid, refs[i].id, refs[i].uuid, next)
		if err != nil {
			return 0, err
		}
		next, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
	}
	return pid, nil
}
