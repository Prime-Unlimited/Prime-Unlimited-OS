// Examines only the previously saved on-device baseline, never the live library.
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
	"os"
	"path/filepath"
	"strings"
	"time"
)

const base = "/data/prime-unlimited/backups/usb-copy/internal-20261121T081110Z"

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func integrity(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, e := db.QueryContext(ctx, "PRAGMA integrity_check")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func fingerprint(ctx context.Context, db *sql.DB) (map[string]string, error) {
	result := map[string]string{}
	rows, e := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if e != nil {
		return nil, e
	}
	names := []string{}
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			rows.Close()
			return nil, e
		}
		names = append(names, s)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, name := range names {
		rows, e = db.QueryContext(ctx, "SELECT * FROM "+quote(name)+" LIMIT 0")
		if e != nil {
			return nil, e
		}
		cols, e := rows.Columns()
		rows.Close()
		if e != nil {
			return nil, e
		}
		order := make([]string, len(cols))
		for i, col := range cols {
			order[i] = quote(col)
		}
		rows, e = db.QueryContext(ctx, "SELECT * FROM "+quote(name)+" ORDER BY "+strings.Join(order, ","))
		if e != nil {
			return nil, e
		}
		h := sha256.New()
		for rows.Next() {
			v := make([]any, len(cols))
			ptr := make([]any, len(cols))
			for i := range v {
				ptr[i] = &v[i]
			}
			if e = rows.Scan(ptr...); e != nil {
				rows.Close()
				return nil, e
			}
			data, e := json.Marshal(v)
			if e != nil {
				rows.Close()
				return nil, e
			}
			h.Write(data)
			h.Write([]byte{'\n'})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		result[name] = hex.EncodeToString(h.Sum(nil))
	}
	rows, e = db.QueryContext(ctx, "SELECT type,name,sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY type,name")
	if e != nil {
		return nil, e
	}
	h := sha256.New()
	for rows.Next() {
		var a, b, c string
		if e = rows.Scan(&a, &b, &c); e != nil {
			rows.Close()
			return nil, e
		}
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", a, b, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	result["__schema__"] = hex.EncodeToString(h.Sum(nil))
	var version int
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return nil, e
	}
	result["__user_version__"] = fmt.Sprint(version)
	return result, nil
}
func run() error {
	selectedBase := base
	if len(os.Args) != 1 {
		if len(os.Args) != 3 || os.Args[1] != "--fresh-backup" {
			return errors.New("unsupported arguments")
		}
		selectedBase = filepath.Clean(os.Args[2])
		if !strings.HasPrefix(selectedBase, "/data/prime-unlimited/backups/usb-copy/internal-") || filepath.Dir(selectedBase) != "/data/prime-unlimited/backups/usb-copy" {
			return errors.New("backup path not allowed")
		}
		real, e := filepath.EvalSymlinks(selectedBase)
		if e != nil || real != selectedBase {
			return errors.New("indirect backup path")
		}
		if _, e = os.Stat(filepath.Join(selectedBase, "COMPLETE")); e != nil {
			return errors.New("incomplete backup")
		}
	}
	base := selectedBase

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	src, e := sql.Open("sqlite", "file:"+base+"/files/m.db?mode=ro")
	if e != nil {
		return e
	}
	defer src.Close()
	src.SetMaxOpenConns(1)
	original, e := integrity(ctx, src)
	if e != nil {
		return e
	}
	before, e := fingerprint(ctx, src)
	if e != nil {
		return e
	}
	dir := filepath.Join(base, "rebuild-check-"+time.Now().UTC().Format("20060102T150405Z"))
	if e = os.Mkdir(dir, 0700); e != nil {
		return e
	}
	target := filepath.Join(dir, "m.db")
	// VACUUM INTO reads the baseline and writes only a new destination database.
	if _, e = src.ExecContext(ctx, "VACUUM INTO ?", target); e != nil {
		return e
	}
	dst, e := sql.Open("sqlite", "file:"+target+"?mode=ro")
	if e != nil {
		return e
	}
	defer dst.Close()
	dst.SetMaxOpenConns(1)
	after, e := fingerprint(ctx, dst)
	if e != nil {
		return e
	}
	rebuilt, e := integrity(ctx, dst)
	if e != nil {
		return e
	}
	equal := len(before) == len(after)
	for k, v := range before {
		if after[k] != v {
			equal = false
		}
	}
	report := struct {
		OriginalIntegrity, RebuiltIntegrity []string
		LogicalContentAndSchemaEqual        bool
		ComparedTables                      int
		CandidatePath                       string
	}{original, rebuilt, equal, len(before) - 2, target}
	b, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(dir, "report.json"), b, 0600); e != nil {
		return e
	}
	fmt.Println(string(b))
	if !equal {
		return errors.New("logical content changed")
	}
	if len(rebuilt) != 1 || rebuilt[0] != "ok" {
		return errors.New("rebuilt integrity failed")
	}
	if e = os.WriteFile(filepath.Join(base, "VERIFIED_CANDIDATE"), []byte(target+"\n"), 0600); e != nil {
		return e
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
