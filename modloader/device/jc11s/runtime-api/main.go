package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type catalog struct {
	SchemaVersion int              `json:"schemaVersion"`
	Mods          []map[string]any `json:"mods"`
}

type server struct {
	model    string
	firmware string
	catalog  catalog
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func stringList(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *server) compatible(mod map[string]any) bool {
	return contains(stringList(mod["models"]), s.model) &&
		contains(stringList(mod["firmware"]), s.firmware) &&
		mod["revoked"] != true
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameters are not supported"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "mutation authorization required"})
		return
	}
	switch r.URL.Path {
	case "/api/v1/health":
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "prime-unlimited-mod-loader", "mode": "read-only-device-runtime",
		})
	case "/api/v1/mods":
		mods := make([]map[string]any, 0, len(s.catalog.Mods))
		for _, original := range s.catalog.Mods {
			copy := make(map[string]any, len(original)+1)
			for key, value := range original {
				copy[key] = value
			}
			copy["compatible"] = s.compatible(original)
			mods = append(mods, copy)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"model": s.model, "firmware": s.firmware, "mods": mods,
		})
	case "/api/v1/installed":
		writeJSON(w, http.StatusOK, map[string]any{"mods": map[string]any{}})
	case "/api/v1/transactions":
		writeJSON(w, http.StatusOK, map[string]any{"transactions": []any{}})
	case "/api/v1/libraries":
		writeJSON(w, http.StatusOK, map[string]any{"libraries": []any{}})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func loadCatalog(path string) (catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return catalog{SchemaVersion: 1, Mods: []map[string]any{}}, nil
		}
		return catalog{}, err
	}
	if len(data) > 1024*1024 {
		return catalog{}, fmt.Errorf("catalog exceeds 1 MiB")
	}
	var result catalog
	if err := json.Unmarshal(data, &result); err != nil {
		return catalog{}, err
	}
	if (result.SchemaVersion != 1 && result.SchemaVersion != 2) || result.Mods == nil {
		return catalog{}, fmt.Errorf("unsupported catalog")
	}
	for _, mod := range result.Mods {
		for _, key := range []string{"id", "name", "version", "downloadUrl", "sha256"} {
			text, ok := mod[key].(string)
			if !ok || strings.TrimSpace(text) == "" {
				return catalog{}, fmt.Errorf("invalid catalog field %s", key)
			}
		}
	}
	return result, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8098", "loopback listen address")
	model := flag.String("model", "JC11S", "device model")
	firmware := flag.String("firmware", "5.3.0", "Engine OS version")
	catalogPath := flag.String("catalog", "/data/prime-unlimited/catalog/catalog.json", "catalog file")
	flag.Parse()
	if !strings.HasPrefix(*listen, "127.0.0.1:") {
		log.Fatal("refusing non-loopback listen address")
	}
	loaded, err := loadCatalog(*catalogPath)
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           &server{model: *model, firmware: *firmware, catalog: loaded},
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       15 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	log.Printf("Prime Unlimited runtime API listening on http://%s", *listen)
	log.Fatal(httpServer.ListenAndServe())
}
