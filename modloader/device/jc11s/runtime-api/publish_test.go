package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDispatchPromotionUsesDeviceDigestAndReleaseBranch(t *testing.T) {
	var payload struct {
		Ref    string            `json:"ref"`
		Inputs map[string]string `json:"inputs"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/actions/workflows/mod-promote.yml/dispatches" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatal("missing workflow authorization")
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newGitHubDeviceClient("client")
	client.repoURL, client.client = server.URL, server.Client()
	if err := client.dispatchPromotion(context.Background(), "token", "clock", "1.2.3", "jc11s", "digest"); err != nil {
		t.Fatal(err)
	}
	if payload.Ref != "feat/prime-unlimited-mod-loader" || payload.Inputs["mod"] != "clock" || payload.Inputs["jc11s_sha256"] != "digest" || payload.Inputs["jp24_sha256"] != "" {
		t.Fatalf("unexpected dispatch payload: %#v", payload)
	}
}

func TestLatestPromotionReportsCompletedConclusion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflow_runs":[{"status":"completed","conclusion":"failure","created_at":"2026-11-22T16:00:00Z"}]}`))
	}))
	defer server.Close()
	client := newGitHubDeviceClient("client")
	client.repoURL, client.client = server.URL, server.Client()
	status, conclusion, err := client.latestPromotion(context.Background(), "token", time.Date(2026, 11, 22, 15, 59, 59, 0, time.UTC))
	if err != nil || status != "completed" || conclusion != "failure" {
		t.Fatalf("status=%q conclusion=%q err=%v", status, conclusion, err)
	}
}

func TestPublishStateDefaultsToIdleAndTracksProgress(t *testing.T) {
	var state publishState
	if state.snapshot()["status"] != "idle" {
		t.Fatal("zero publish state is not idle")
	}
	state.set("publishing", "clock-jc11s", "Publishing...", 65)
	snapshot := state.snapshot()
	if snapshot["progress"] != 65 || snapshot["modId"] != "clock-jc11s" {
		t.Fatalf("unexpected state: %#v", snapshot)
	}
}

