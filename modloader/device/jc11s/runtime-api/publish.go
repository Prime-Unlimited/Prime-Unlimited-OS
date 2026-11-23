package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type publishState struct {
	mu       sync.Mutex
	Status   string `json:"status"`
	ModID    string `json:"modId,omitempty"`
	Message  string `json:"message,omitempty"`
	Progress int    `json:"progress"`
}

func (p *publishState) set(status, modID, message string, progress int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status, p.ModID, p.Message, p.Progress = status, modID, message, progress
}

func (p *publishState) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.Status
	if status == "" {
		status = "idle"
	}
	return map[string]any{"status": status, "modId": p.ModID, "message": p.Message, "progress": p.Progress}
}

func (c *githubDeviceClient) githubJSON(ctx context.Context, method, endpoint, token string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.repoURL+endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "Prime-Unlimited-Modloader")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return data, response.StatusCode, err
}

func (c *githubDeviceClient) dispatchPromotion(ctx context.Context, token, modID, version, device, digest string) error {
	inputs := map[string]string{"mod": modID, "version": version, "jc11s_sha256": "", "jp24_sha256": ""}
	inputs[device+"_sha256"] = digest
	_, status, err := c.githubJSON(ctx, http.MethodPost, "/actions/workflows/mod-promote.yml/dispatches", token, map[string]any{
		"ref": "feat/prime-unlimited-mod-loader", "inputs": inputs,
	})
	if err != nil || status != http.StatusNoContent {
		return errors.New("GitHub workflow could not be started")
	}
	return nil
}

func (c *githubDeviceClient) latestPromotion(ctx context.Context, token string, started time.Time) (string, string, error) {
	query := url.Values{"branch": {"feat/prime-unlimited-mod-loader"}, "event": {"workflow_dispatch"}, "per_page": {"10"}}
	data, status, err := c.githubJSON(ctx, http.MethodGet, "/actions/workflows/mod-promote.yml/runs?"+query.Encode(), token, nil)
	if err != nil || status != http.StatusOK {
		return "", "", errors.New("GitHub workflow status is unavailable")
	}
	var payload struct {
		Runs []struct {
			Status, Conclusion string
			CreatedAt          time.Time `json:"created_at"`
		} `json:"workflow_runs"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return "", "", errors.New("GitHub workflow status is invalid")
	}
	for _, run := range payload.Runs {
		if !run.CreatedAt.Before(started.Add(-5 * time.Second)) {
			return run.Status, run.Conclusion, nil
		}
	}
	return "queued", "", nil
}

func (s *server) handlePublish(w http.ResponseWriter, r *http.Request) {
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	item, ok := s.lookupMod(request.ID)
	if !ok || item.publicationStatus() != "development" || !s.developerEnabled() {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only a development mod in developer mode can be published"})
		return
	}
	token := s.developerAccessToken()
	if token == "" || item.Release == nil || item.Release.ArtifactSHA256 == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "GitHub developer authorization is required"})
		return
	}
	device := strings.ToLower(s.model)
	suffix := "-" + device
	modID := strings.TrimSuffix(item.ID, suffix)
	if modID == item.ID || (device != "jc11s" && device != "jp24") {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "development mod has no device publication mapping"})
		return
	}
	state := s.publish.snapshot()["status"].(string)
	if state == "publishing" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a mod is already publishing"})
		return
	}
	started := time.Now().UTC()
	s.publish.set("publishing", item.ID, "Starting GitHub workflow", 5)
	if err := s.github.dispatchPromotion(r.Context(), token, modID, item.Version, device, item.Release.ArtifactSHA256); err != nil {
		s.publish.set("failed", item.ID, err.Error(), 0)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	go s.watchPromotion(token, item.ID, started)
	writeJSON(w, http.StatusAccepted, s.publish.snapshot())
}

func (s *server) watchPromotion(token, modID string, started time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.publish.set("failed", modID, "GitHub workflow timed out", 0)
			return
		case <-ticker.C:
			status, conclusion, err := s.github.latestPromotion(ctx, token, started)
			if err != nil {
				s.publish.set("publishing", modID, err.Error(), 20)
				continue
			}
			switch status {
			case "queued", "waiting", "pending":
				s.publish.set("publishing", modID, "Waiting for GitHub workflow", 25)
			case "in_progress":
				s.publish.set("publishing", modID, "Publishing release and signed catalog", 65)
			case "completed":
				if conclusion != "success" {
					s.publish.set("failed", modID, fmt.Sprintf("GitHub workflow failed: %s", conclusion), 0)
					return
				}
				_, _ = s.syncCatalog(context.Background())
				s.publish.set("published", modID, "Mod published", 100)
				return
			}
		}
	}
}

