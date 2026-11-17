package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const maxCatalogBytes = 1024 * 1024

var keyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type trustKey struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

type signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".sign-catalog-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func loadPrivateKey(path string, generate bool) (ed25519.PrivateKey, error) {
	if generate {
		if _, err := os.Lstat(path); err == nil {
			return nil, errors.New("private key already exists; refusing replacement")
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		if err := atomicWrite(path, []byte(hex.EncodeToString(privateKey)+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("private key must be a service-owned regular file with mode 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, err := hex.DecodeString(strings.TrimSuffix(string(data), "\n"))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, errors.New("private key must contain 64 bytes as lowercase hexadecimal")
	}
	if hex.EncodeToString(decoded) != strings.TrimSuffix(string(data), "\n") {
		return nil, errors.New("private key must use lowercase hexadecimal")
	}
	return ed25519.PrivateKey(decoded), nil
}

func marshalDocument(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func main() {
	catalogPath := flag.String("catalog", "", "catalog file to sign")
	privateKeyPath := flag.String("private-key", "", "protected Ed25519 private key file")
	keyID := flag.String("key-id", "", "deployment trust key identifier")
	signaturePath := flag.String("signature-out", "", "detached signature JSON output")
	publicKeyPath := flag.String("public-key-out", "", "public trust key JSON output")
	generate := flag.Bool("generate", false, "generate the private key if it does not exist")
	flag.Parse()
	if *catalogPath == "" || *privateKeyPath == "" || *signaturePath == "" || *publicKeyPath == "" || !keyIDPattern.MatchString(*keyID) {
		flag.Usage()
		os.Exit(2)
	}
	catalogData, err := os.ReadFile(*catalogPath)
	if err != nil || len(catalogData) == 0 || len(catalogData) > maxCatalogBytes {
		fmt.Fprintln(os.Stderr, "catalog is missing, empty, unreadable, or exceeds 1 MiB")
		os.Exit(1)
	}
	privateKey, err := loadPrivateKey(*privateKeyPath, *generate)
	if err != nil {
		fmt.Fprintln(os.Stderr, "private key:", err)
		os.Exit(1)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trustData, err := marshalDocument(trustKey{
		Algorithm: "ed25519", KeyID: *keyID, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	signatureData, err := marshalDocument(signature{
		Algorithm: "ed25519", KeyID: *keyID,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, catalogData)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := atomicWrite(*publicKeyPath, trustData, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "public key output:", err)
		os.Exit(1)
	}
	if err := atomicWrite(*signaturePath, signatureData, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "signature output:", err)
		os.Exit(1)
	}
	keyFingerprint := sha256.Sum256(publicKey)
	catalogDigest := sha256.Sum256(catalogData)
	fmt.Printf("signed catalog key-id=%s key-fingerprint=%s catalog-sha256=%s\n",
		*keyID, hex.EncodeToString(keyFingerprint[:]), hex.EncodeToString(catalogDigest[:]))
}
