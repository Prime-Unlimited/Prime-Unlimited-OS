package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedCredentialsValidation(t *testing.T) {
	valid := map[string]any{"tls": map[string]string{"certificate": "unused", "privateKey": "unused"}, "brokerUrl": "http://127.0.0.1:8098/api/v1/service-splash", "brokerToken": strings.Repeat("a", 64), "webToken": strings.Repeat("b", 64)}
	data, _ := json.Marshal(valid)
	if e := credentials(strings.NewReader(string(data))); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{string(data) + "{}", string(data) + strings.Repeat(" ", 9000), `{"unknown":true}`, strings.Replace(string(data), strings.Repeat("a", 64), strings.Repeat("z", 64), 1), strings.Replace(string(data), "127.0.0.1:8098", "example.com", 1)} {
		if e := credentials(strings.NewReader(bad)); e == nil {
			t.Fatal("invalid managed input accepted")
		}
	}
}
