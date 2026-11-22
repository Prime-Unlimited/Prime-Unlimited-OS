// Sandboxed lifecycle process for the preview media-import capability.
// All media access stays in the trusted runtime; this process opens no media.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func credentials(r io.Reader) error {
	var config struct {
		TLS         struct{ Certificate, PrivateKey string }
		BrokerURL   string
		BrokerToken string
		WebToken    string
	}
	data, err := io.ReadAll(io.LimitReader(r, 8193))
	if err != nil || len(data) > 8192 {
		return errors.New("managed configuration exceeds limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return errors.New("invalid managed configuration")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected managed configuration data")
	}
	if config.BrokerURL != "http://127.0.0.1:8098/api/v1/service-splash" || len(config.BrokerToken) != 64 || len(config.WebToken) != 64 {
		return errors.New("invalid managed capability credentials")
	}
	for _, token := range []string{config.BrokerToken, config.WebToken} {
		decoded, e := hex.DecodeString(token)
		if e != nil || len(decoded) != 32 {
			return errors.New("invalid managed token")
		}
	}
	// The shared runtime currently supplies its generic broker URL. The preview
	// service intentionally does not contact that splash-specific broker.
	return nil
}
func main() {
	managed := flag.Bool("managed", false, "read trusted lifecycle configuration")
	flag.Parse()
	if !*managed || flag.NArg() != 0 {
		os.Exit(2)
	}
	if credentials(os.Stdin) != nil {
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	<-ctx.Done()
}
