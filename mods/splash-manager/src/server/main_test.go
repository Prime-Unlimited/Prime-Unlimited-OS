package main

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedUploadUsesOnlyScopedBrokerCredential(t *testing.T) {
	oldCredentials, oldToken := managedCredentials, authToken
	t.Cleanup(func() { managedCredentials, authToken = oldCredentials, oldToken })
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	calls := 0
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer scoped-broker-token" {
			t.Error("wrong credential sent to broker")
		}
		actual, _ := io.ReadAll(r.Body)
		if !bytes.Equal(actual, pngData.Bytes()) {
			t.Error("PNG changed during forwarding")
		}
		_, _ = io.WriteString(w, "saved")
	}))
	defer broker.Close()
	managedCredentials = &serviceCredentials{BrokerURL: broker.URL, BrokerToken: "scoped-broker-token"}
	authToken = "web-token"
	handler := requireToken(http.HandlerFunc(handleUpload))
	for _, token := range []string{"wrong", "web-token"} {
		request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(pngData.Bytes()))
		request.Header.Set("Content-Type", "image/png")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if token == "wrong" && response.Code != 401 {
			t.Fatal("unauthorized browser upload accepted")
		}
		if token == "web-token" && (response.Code != 200 || response.Body.String() != "saved") {
			t.Fatal("authorized upload failed")
		}
	}
	if calls != 1 {
		t.Fatalf("unexpected broker calls: %d", calls)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("not a PNG"))
	request.Header.Set("Content-Type", "image/png")
	request.Header.Set("Authorization", "Bearer web-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 400 || calls != 1 {
		t.Fatal("invalid PNG reached broker")
	}
}
