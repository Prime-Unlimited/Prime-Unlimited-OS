package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type serviceTLS struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
}

func loadServiceTLS(root, id string) (serviceTLS, error) {
	var config serviceTLS
	path := filepath.Join(root, "services", "tls", serviceName(id)+".json")
	data, err := readOwnedFile(path, 16384, "service TLS")
	if err == nil {
		if decodeStrictJSON(data, &config) != nil {
			return config, fmt.Errorf("invalid service TLS")
		}
		return config, nil
	}
	if !os.IsNotExist(err) {
		return config, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return config, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return config, err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Prime Unlimited device"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	addresses, _ := net.InterfaceAddrs()
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		}
	}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return config, err
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return config, err
	}
	config = serviceTLS{Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})), PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}))}
	return config, writeAtomicJSON(path, config)
}

func (s *server) handleServiceAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	request, err := decodeCacheRequest(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	item, ok := state.Mods[request.ID]
	safety, err := loadSafetyState(s.root)
	if err != nil || !ok || !item.Enabled || item.Service == nil || safety.SafeMode || safety.Mods[request.ID].Quarantined {
		writeJSON(w, 409, map[string]string{"error": "service not enabled"})
		return
	}
	if !s.serviceApproved(item) {
		writeJSON(w, 403, map[string]string{"error": "service no longer approved"})
		return
	}
	record, err := loadServiceRecord(s.root, request.ID)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "service unavailable"})
		return
	}
	block, _ := pem.Decode([]byte(record.Credentials.TLS.Certificate))
	if block == nil {
		writeJSON(w, 503, map[string]string{"error": "TLS unavailable"})
		return
	}
	digest := sha256.Sum256(block.Bytes)
	addresses, _ := net.InterfaceAddrs()
	urls := []string{}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && !ip.IsLoopback() {
			urls = append(urls, "https://"+ip.String()+":8443")
		}
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "pairing unavailable"})
		return
	}
	record.PairingCode = fmt.Sprintf("%06d", number.Int64())
	record.PairingExpires = time.Now().Add(2 * time.Minute).Unix()
	record.PairingAttempts = 0
	if err := writeAtomicJSON(serviceRecordPath(s.root, request.ID), record); err != nil {
		writeJSON(w, 503, map[string]string{"error": "pairing unavailable"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"urls": urls, "pairingCode": record.PairingCode, "expiresIn": 120, "certificateSHA256": hex.EncodeToString(digest[:])})
}

// Only the running service's scoped broker credential can attempt pairing.
// Codes are single-use, expire after two minutes, and permit five attempts.
func (s *server) handleServicePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
		writeJSON(w, 400, map[string]string{"error": "invalid pairing request"})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid pairing request"})
		return
	}
	var request struct {
		Code string `json:"code"`
	}
	if decodeStrictJSON(data, &request) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid pairing request"})
		return
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		writeJSON(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	provided := strings.TrimPrefix(values[0], "Bearer ")
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	state, err := loadRegistry(filepath.Join(s.root, "registry", "state.json"))
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "unavailable"})
		return
	}
	safety, err := loadSafetyState(s.root)
	if err != nil || safety.SafeMode {
		writeJSON(w, 403, map[string]string{"error": "blocked"})
		return
	}
	for id, item := range state.Mods {
		if !item.Enabled || item.Service == nil || safety.Mods[id].Quarantined || !s.serviceApproved(item) {
			continue
		}
		record, err := loadServiceRecord(s.root, id)
		if err != nil || record.PackageSHA256 != item.PackageSHA256 || len(provided) != 64 || subtle.ConstantTimeCompare([]byte(provided), []byte(record.Credentials.BrokerToken)) != 1 {
			continue
		}
		if record.PairingCode == "" || record.PairingExpires <= time.Now().Unix() || record.PairingAttempts >= 5 {
			writeJSON(w, 429, map[string]string{"error": "Request a new code on the device"})
			return
		}
		record.PairingAttempts++
		valid := len(request.Code) == 6 && subtle.ConstantTimeCompare([]byte(request.Code), []byte(record.PairingCode)) == 1
		if valid {
			record.PairingCode = ""
			record.PairingExpires = 0
		}
		if err := writeAtomicJSON(serviceRecordPath(s.root, id), record); err != nil {
			writeJSON(w, 503, map[string]string{"error": "unavailable"})
			return
		}
		if !valid {
			writeJSON(w, 401, map[string]string{"error": "Code incorrect. Check WEB ACCESS on the device"})
			return
		}
		writeJSON(w, 200, map[string]string{"token": record.Credentials.WebToken})
		return
	}
	writeJSON(w, 401, map[string]string{"error": "unauthorized"})
}
