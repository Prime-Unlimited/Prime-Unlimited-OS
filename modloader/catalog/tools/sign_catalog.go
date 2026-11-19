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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const maxCatalogBytes = 1024 * 1024
const maxMetadataBytes = 4096

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

type verificationResult struct {
	KeyID          string
	KeyFingerprint string
	CatalogDigest  string
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}

func readCatalog(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > maxCatalogBytes {
		return nil, errors.New("catalog is missing, empty, unreadable, or exceeds 1 MiB")
	}
	return data, nil
}

func readOwnedFile(path string, limit int64, label string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0o022 != 0 {
		return nil, fmt.Errorf("%s must be an owner-owned regular file without group or world write access", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be opened", label)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s cannot be read", label)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds size limit", label)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, target any) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("invalid JSON object key")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("unexpected JSON delimiter")
		}
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("trailing JSON data")
	}
	return nil
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

func signCatalog(catalogPath, privateKeyPath, keyID, signaturePath, publicKeyPath string, generate bool) (verificationResult, error) {
	if !keyIDPattern.MatchString(keyID) {
		return verificationResult{}, errors.New("invalid key ID")
	}
	catalogData, err := readCatalog(catalogPath)
	if err != nil {
		return verificationResult{}, err
	}
	privateKey, err := loadPrivateKey(privateKeyPath, generate)
	if err != nil {
		return verificationResult{}, fmt.Errorf("private key: %w", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trustData, err := marshalDocument(trustKey{
		Algorithm: "ed25519", KeyID: keyID, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	if err != nil {
		return verificationResult{}, err
	}
	signatureData, err := marshalDocument(signature{
		Algorithm: "ed25519", KeyID: keyID,
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, catalogData)),
	})
	if err != nil {
		return verificationResult{}, err
	}
	if err := atomicWrite(publicKeyPath, trustData, 0o644); err != nil {
		return verificationResult{}, fmt.Errorf("public key output: %w", err)
	}
	if err := atomicWrite(signaturePath, signatureData, 0o644); err != nil {
		return verificationResult{}, fmt.Errorf("signature output: %w", err)
	}
	return resultFor(catalogData, keyID, publicKey), nil
}

func resultFor(catalogData []byte, keyID string, publicKey ed25519.PublicKey) verificationResult {
	keyFingerprint := sha256.Sum256(publicKey)
	catalogDigest := sha256.Sum256(catalogData)
	return verificationResult{
		KeyID:          keyID,
		KeyFingerprint: hex.EncodeToString(keyFingerprint[:]),
		CatalogDigest:  hex.EncodeToString(catalogDigest[:]),
	}
}

func verifyCatalog(catalogPath, signaturePath, publicKeyPath string) (verificationResult, error) {
	catalogData, err := readOwnedFile(catalogPath, maxCatalogBytes, "catalog")
	if err != nil {
		return verificationResult{}, fmt.Errorf("catalog unavailable or unsafe: %w", err)
	}
	if len(catalogData) == 0 {
		return verificationResult{}, errors.New("catalog is empty")
	}
	trustData, err := readOwnedFile(publicKeyPath, maxMetadataBytes, "catalog trust key")
	if err != nil {
		return verificationResult{}, fmt.Errorf("catalog trust key unavailable or unsafe: %w", err)
	}
	signatureData, err := readOwnedFile(signaturePath, maxMetadataBytes, "catalog signature")
	if err != nil {
		return verificationResult{}, fmt.Errorf("catalog signature unavailable or unsafe: %w", err)
	}
	var trust trustKey
	var signed signature
	if err := decodeStrictJSON(trustData, &trust); err != nil {
		return verificationResult{}, errors.New("invalid catalog trust metadata")
	}
	if err := decodeStrictJSON(signatureData, &signed); err != nil {
		return verificationResult{}, errors.New("invalid catalog signature metadata")
	}
	if trust.Algorithm != "ed25519" || signed.Algorithm != "ed25519" ||
		!keyIDPattern.MatchString(trust.KeyID) || signed.KeyID != trust.KeyID {
		return verificationResult{}, errors.New("catalog trust metadata mismatch")
	}
	publicKey, err := base64.StdEncoding.DecodeString(trust.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return verificationResult{}, errors.New("invalid catalog public key")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signed.Signature)
	if err != nil || len(signatureBytes) != ed25519.SignatureSize ||
		!ed25519.Verify(ed25519.PublicKey(publicKey), catalogData, signatureBytes) {
		return verificationResult{}, errors.New("catalog signature verification failed")
	}
	return resultFor(catalogData, trust.KeyID, ed25519.PublicKey(publicKey)), nil
}

func main() {
	catalogPath := flag.String("catalog", "", "catalog file to sign or verify")
	privateKeyPath := flag.String("private-key", "", "protected Ed25519 private key file")
	keyID := flag.String("key-id", "", "deployment trust key identifier")
	signaturePath := flag.String("signature-out", "", "detached signature JSON output")
	publicKeyPath := flag.String("public-key-out", "", "public trust key JSON output")
	generate := flag.Bool("generate", false, "generate the private key if it does not exist")
	verify := flag.Bool("verify", false, "verify an existing detached catalog signature")
	verifySignaturePath := flag.String("signature", "", "detached signature JSON input for -verify")
	verifyPublicKeyPath := flag.String("public-key", "", "public trust key JSON input for -verify")
	flag.Parse()
	if *verify {
		if *catalogPath == "" || *verifySignaturePath == "" || *verifyPublicKeyPath == "" || *generate || *privateKeyPath != "" || *keyID != "" || *signaturePath != "" || *publicKeyPath != "" {
			flag.Usage()
			os.Exit(2)
		}
		result, err := verifyCatalog(*catalogPath, *verifySignaturePath, *verifyPublicKeyPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "verification:", err)
			os.Exit(1)
		}
		fmt.Printf("verified catalog key-id=%s key-fingerprint=%s catalog-sha256=%s\n",
			result.KeyID, result.KeyFingerprint, result.CatalogDigest)
		return
	}
	if *catalogPath == "" || *privateKeyPath == "" || *signaturePath == "" || *publicKeyPath == "" || !keyIDPattern.MatchString(*keyID) || *verifySignaturePath != "" || *verifyPublicKeyPath != "" {
		flag.Usage()
		os.Exit(2)
	}
	result, err := signCatalog(*catalogPath, *privateKeyPath, *keyID, *signaturePath, *publicKeyPath, *generate)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("signed catalog key-id=%s key-fingerprint=%s catalog-sha256=%s\n",
		result.KeyID, result.KeyFingerprint, result.CatalogDigest)
}
