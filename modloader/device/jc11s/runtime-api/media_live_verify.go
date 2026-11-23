package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	usbcopy "prime-unlimited.usb-copy/worker"
)

type liveRegisteredTrack struct {
	ID       int64
	Origin   nativePlaylistMember
	Relative string
}

func liveTrackPath(t usbcopy.PlannedTrack) string {
	return filepath.ToSlash(filepath.Join("Music", t.DestinationRelative))
}
func liveVerifyArtwork(root string, hash []byte) error {
	if len(hash) != sha1.Size {
		return errors.New("native artwork hash is missing")
	}
	dir := filepath.Join(root, "Engine Library", "Artwork")
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("native artwork directory unavailable")
	}
	path := filepath.Join(dir, base64.RawURLEncoding.EncodeToString(hash)+".jpg")
	f, e := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e = f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > 32<<20 {
		return errors.New("invalid native artwork file")
	}
	cfg, format, e := image.DecodeConfig(f)
	if e != nil || format != "jpeg" || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 4096 || cfg.Height > 4096 {
		return errors.New("native artwork is not a supported JPEG")
	}
	return nil
}
func liveRegistered(ctx context.Context, database, root, uuid string, t usbcopy.PlannedTrack) (liveRegisteredTrack, error) {
	var out liveRegisteredTrack
	db, e := openNativeReadDB(database)
	if e != nil {
		return out, e
	}
	defer db.Close()
	rows, e := db.QueryContext(ctx, `SELECT t.id,coalesce(t.title,''),coalesce(t.artist,''),coalesce(t.album,''),coalesce(t.genre,''),coalesce(t.comment,''),coalesce(t.composer,''),coalesce(t.year,0),coalesce(t.fileBytes,0),coalesce(a.hash,x''),t.originDatabaseUuid,t.originTrackId FROM Track t LEFT JOIN AlbumArt a ON a.id=t.albumArtId WHERE t.path=?`, liveTrackPath(t))
	if e != nil {
		return out, e
	}
	n := 0
	var title, artist, album, genre, comment, composer string
	var year, fileBytes int64
	var art []byte
	for rows.Next() {
		n++
		e = rows.Scan(&out.ID, &title, &artist, &album, &genre, &comment, &composer, &year, &fileBytes, &art, &out.Origin.UUID, &out.Origin.Track)
		if e != nil {
			rows.Close()
			return out, e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if n != 1 || out.ID <= 0 || out.ID >= (1<<48) || !strings.EqualFold(out.Origin.UUID, uuid) || out.Origin.Track != out.ID {
		return out, errors.New("native registration identity is missing or ambiguous")
	}
	out.Origin.UUID = strings.ToLower(out.Origin.UUID)
	out.Relative = liveTrackPath(t)
	if fileBytes != t.Bytes {
		return out, errors.New("native registration file size differs")
	}
	if t.Tags.Title != "" {
		if title != t.Tags.Title {
			return out, errors.New("native title differs from ID3 title")
		}
	} else {
		base := filepath.Base(t.SourceRelative)
		if title != base && title != strings.TrimSuffix(base, filepath.Ext(base)) {
			return out, errors.New("native filename title is missing")
		}
	}
	for _, pair := range [][2]string{{t.Tags.Artist, artist}, {t.Tags.Album, album}, {t.Tags.Genre, genre}, {t.Tags.Comment, comment}, {t.Tags.Composer, composer}} {
		if pair[0] != "" && pair[0] != pair[1] {
			return out, errors.New("native ID3 metadata differs")
		}
	}
	if t.Tags.Year > 0 && year != int64(t.Tags.Year) {
		return out, errors.New("native ID3 year differs")
	}
	if len(t.Artwork) > 0 {
		expected := sha1.Sum(t.Artwork)
		if !bytes.Equal(art, expected[:]) {
			return out, errors.New("native artwork association differs from embedded cover")
		}
		if e = liveVerifyArtwork(root, art); e != nil {
			return out, e
		}
	}
	return out, nil
}
func liveCheckNativeTrack(ctx context.Context, database, root string, registered liveRegisteredTrack, ref usbcopy.LiveTrackReference, raw string, binding usbcopy.LiveBinding) error {
	if ref.Binding != binding || ref.LocalID != registered.ID {
		return errors.New("native lookup returned a different track")
	}
	state := nativeCreateState{LookupLocalID: registered.ID, LookupOriginUUID: registered.Origin.UUID, LookupOriginID: registered.Origin.Track, LookupFileURL: (&url.URL{Scheme: "file", Path: filepath.Join(root, "Engine Library", registered.Relative)}).String()}
	expected, e := nativeLookupFileURL(ctx, database, root, registered.ID)
	if e != nil || expected != state.LookupFileURL {
		return errors.New("registered path changed during native lookup")
	}
	if e = populateNativeLookupOrigin(ctx, &state, database); e != nil {
		return e
	}
	if state.LookupOriginUUID != registered.Origin.UUID || state.LookupOriginID != registered.Origin.Track {
		return errors.New("registered origin changed during native lookup")
	}
	if e = verifyNativeLookupResult(&state, ref.NativeID, raw); e != nil {
		return e
	}
	if state.Phase != "verified" {
		return errors.New("native track URI did not match registered file")
	}
	return nil
}
func liveMemberKey(m nativePlaylistMember) string {
	return strings.ToLower(m.UUID) + "\x00" + strconv.FormatInt(m.Track, 10)
}
func liveMembers(ctx context.Context, db *sql.DB, id int64) (map[string]int, error) {
	rows, e := db.QueryContext(ctx, "SELECT databaseUuid,trackId FROM PlaylistEntity WHERE listId=? ORDER BY id", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var m nativePlaylistMember
		if e = rows.Scan(&m.UUID, &m.Track); e != nil {
			return nil, e
		}
		if m.UUID == "" || m.Track <= 0 {
			return nil, errors.New("invalid native playlist member")
		}
		out[liveMemberKey(m)]++
	}
	return out, rows.Err()
}
func liveExactMembers(actual, expected map[string]int) error {
	if len(actual) != len(expected) {
		return errors.New("native playlist membership count differs")
	}
	for k, n := range expected {
		if n != 1 || actual[k] != n {
			return errors.New("native playlist contains unexpected or repeated tracks")
		}
	}
	return nil
}

// Read-only polling permits a native transaction/artwork write to settle. It
// never resubmits a command and stops immediately on cancellation.
func liveWaitProof(ctx context.Context, proof func() error) error {
	limit, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var last error
	for {
		if last = proof(); last == nil {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-limit.Done():
			timer.Stop()
			return fmt.Errorf("native result verification: %w", errors.Join(last, limit.Err()))
		case <-timer.C:
		}
	}
}
