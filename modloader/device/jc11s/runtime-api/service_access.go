package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"urls": urls, "token": record.Credentials.WebToken, "certificateSHA256": hex.EncodeToString(digest[:])})
}
