package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const githubOAuthClientID = "Ov23liVlFBJSp9nmohwz"
const githubAdminRepository = "Prime-Unlimited/Prime-Unlimited-OS"

type githubDeviceClient struct {
	clientID string
	codeURL  string
	tokenURL string
	repoURL  string
	client   *http.Client
}

type githubDeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresIn       int
	Interval        int
}

type developerLogin struct {
	mu              sync.Mutex
	status          string
	userCode        string
	verificationURI string
	message         string
	cancel          context.CancelFunc
}

func newGitHubDeviceClient(clientID string) *githubDeviceClient {
	return &githubDeviceClient{
		clientID: clientID,
		codeURL:  "https://github.com/login/device/code",
		tokenURL: "https://github.com/login/oauth/access_token",
		repoURL:  "https://api.github.com/repos/" + githubAdminRepository,
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *githubDeviceClient) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "Prime-Unlimited-Modloader")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

func (c *githubDeviceClient) requestCode(ctx context.Context) (githubDeviceCode, error) {
	form := url.Values{"client_id": {c.clientID}}
	body, status, err := c.postForm(ctx, c.codeURL, form)
	if err != nil {
		return githubDeviceCode{}, errors.New("GitHub device login is unavailable")
	}
	var payload struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		Description     string `json:"error_description"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return githubDeviceCode{}, errors.New("GitHub device login is unavailable")
	}
	if status == http.StatusNotFound {
		return githubDeviceCode{}, errors.New("GitHub app was not found")
	}
	if payload.Error == "device_flow_disabled" || status == http.StatusBadRequest && payload.Error != "" {
		if payload.Description != "" {
			return githubDeviceCode{}, errors.New(payload.Description)
		}
		return githubDeviceCode{}, errors.New("GitHub device login is disabled")
	}
	if payload.Error != "" || payload.DeviceCode == "" || payload.UserCode == "" {
		return githubDeviceCode{}, errors.New("GitHub device login is unavailable")
	}
	if payload.VerificationURI == "" {
		payload.VerificationURI = "https://github.com/login/device"
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 900
	}
	if payload.Interval <= 0 {
		payload.Interval = 5
	}
	return githubDeviceCode{
		DeviceCode: payload.DeviceCode, UserCode: payload.UserCode,
		VerificationURI: payload.VerificationURI, ExpiresIn: payload.ExpiresIn, Interval: payload.Interval,
	}, nil
}

func (c *githubDeviceClient) exchange(ctx context.Context, deviceCode string) (string, string, time.Duration, error) {
	form := url.Values{
		"client_id":   {c.clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	body, _, err := c.postForm(ctx, c.tokenURL, form)
	if err != nil {
		if ctx.Err() != nil {
			return "", "cancelled", 0, ctx.Err()
		}
		return "", "error", 0, errors.New("GitHub authorization failed")
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
		Interval    int    `json:"interval"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", "error", 0, errors.New("GitHub authorization failed")
	}
	switch payload.Error {
	case "":
		if payload.AccessToken == "" {
			return "", "error", 0, errors.New("GitHub authorization failed")
		}
		return payload.AccessToken, "ok", 0, nil
	case "authorization_pending":
		return "", "pending", 0, nil
	case "slow_down":
		wait := time.Duration(payload.Interval) * time.Second
		if wait <= 0 {
			wait = 5 * time.Second
		}
		return "", "slow_down", wait, nil
	case "expired_token":
		return "", "expired", 0, nil
	case "access_denied":
		return "", "denied", 0, nil
	default:
		if payload.Description != "" {
			return "", "error", 0, errors.New(payload.Description)
		}
		return "", "error", 0, errors.New("GitHub authorization failed")
	}
}

func (c *githubDeviceClient) repositoryAdmin(ctx context.Context, token string) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.repoURL, nil)
	if err != nil {
		return false, errors.New("GitHub repository lookup failed")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("User-Agent", "Prime-Unlimited-Modloader")
	response, err := c.client.Do(request)
	if err != nil {
		return false, errors.New("GitHub repository lookup failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		return false, errors.New("GitHub repository lookup failed")
	}
	var payload struct {
		Permissions struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false, errors.New("GitHub repository lookup failed")
	}
	return payload.Permissions.Admin, nil
}

func (s *server) cancelDeveloperLogin() {
	s.login.mu.Lock()
	defer s.login.mu.Unlock()
	if s.login.cancel != nil {
		s.login.cancel()
		s.login.cancel = nil
	}
	s.login.status = "idle"
	s.login.userCode = ""
	s.login.verificationURI = ""
	s.login.message = ""
}

func (s *server) developerModeStatus() map[string]any {
	enabled := s.developerEnabled()
	s.login.mu.Lock()
	defer s.login.mu.Unlock()
	status := s.login.status
	if enabled {
		status = "enabled"
	} else if status == "" || status == "enabled" {
		status = "idle"
	}
	return map[string]any{
		"enabled":         enabled,
		"configured":      s.github != nil,
		"status":          status,
		"userCode":        s.login.userCode,
		"verificationUri": s.login.verificationURI,
		"message":         s.login.message,
	}
}

func (s *server) beginDeveloperLogin() (int, map[string]any) {
	if s.github == nil {
		return http.StatusForbidden, map[string]any{"error": "developer authorization is not configured"}
	}
	s.cancelDeveloperLogin()
	code, err := s.github.requestCode(context.Background())
	if err != nil {
		return http.StatusBadGateway, map[string]any{"error": err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(code.ExpiresIn)*time.Second)
	s.login.mu.Lock()
	s.login.status = "pending"
	s.login.userCode = code.UserCode
	s.login.verificationURI = code.VerificationURI
	s.login.message = ""
	s.login.cancel = cancel
	s.login.mu.Unlock()
	go s.pollDeveloperLogin(ctx, code.DeviceCode, time.Duration(code.Interval)*time.Second)
	return http.StatusOK, s.developerModeStatus()
}

func (s *server) enableDeveloperModeFromLogin(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.login.mu.Lock()
	defer s.login.mu.Unlock()
	if ctx.Err() != nil || s.login.cancel == nil {
		return context.Canceled
	}
	return s.setDeveloperEnabled(true)
}

func (s *server) finishDeveloperLogin(status, message string) {
	s.login.mu.Lock()
	defer s.login.mu.Unlock()
	if s.login.cancel != nil {
		s.login.cancel()
		s.login.cancel = nil
	}
	s.login.status = status
	s.login.message = message
	if status != "pending" {
		s.login.userCode = ""
	}
}

func (s *server) pollDeveloperLogin(ctx context.Context, deviceCode string, interval time.Duration) {
	if interval < time.Second {
		interval = 5 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		token, kind, retry, err := s.github.exchange(ctx, deviceCode)
		if ctx.Err() != nil {
			return
		}
		switch kind {
		case "pending":
			timer.Reset(interval)
		case "slow_down":
			if retry > interval {
				interval = retry
			} else {
				interval += 5 * time.Second
			}
			timer.Reset(interval)
		case "expired":
			s.finishDeveloperLogin("expired", "GitHub code expired")
			return
		case "denied":
			s.finishDeveloperLogin("denied", "GitHub authorization was denied")
			return
		case "ok":
			admin, adminErr := s.github.repositoryAdmin(ctx, token)
			token = ""
			if ctx.Err() != nil {
				return
			}
			if adminErr != nil {
				s.finishDeveloperLogin("error", adminErr.Error())
				return
			}
			if !admin {
				s.finishDeveloperLogin("denied", "GitHub account is not a repository admin")
				return
			}
			if err := s.enableDeveloperModeFromLogin(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				s.finishDeveloperLogin("error", "developer mode update failed")
				return
			}
			s.finishDeveloperLogin("enabled", "")
			return
		default:
			message := "GitHub authorization failed"
			if err != nil {
				message = err.Error()
			}
			s.finishDeveloperLogin("error", message)
			return
		}
	}
}
