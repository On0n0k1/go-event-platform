package tlsconfig

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateSelfSignedCert produces a throwaway self-signed cert/key pair.
// Server/Client only parse these files at load time (real chain validation
// happens later, during the actual TLS handshake), so a self-signed cert is
// sufficient to exercise the loading logic.
func generateSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

func writeTempFile(t *testing.T, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.pem")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestServerLoadsValidCertKeyAndCA(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)
	certFile := writeTempFile(t, certPEM)
	keyFile := writeTempFile(t, keyPEM)
	caFile := writeTempFile(t, certPEM)

	cfg, err := Server(certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates = %d, want 1", len(cfg.Certificates))
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Error("ClientCAs is nil, want a populated pool")
	}
}

func TestServerFailsOnMissingCertFile(t *testing.T) {
	_, keyPEM := generateSelfSignedCert(t)
	keyFile := writeTempFile(t, keyPEM)

	if _, err := Server(filepath.Join(t.TempDir(), "missing.crt"), keyFile, keyFile); err == nil {
		t.Fatal("expected error for missing cert file")
	}
}

func TestServerFailsOnMissingCAFile(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)
	certFile := writeTempFile(t, certPEM)
	keyFile := writeTempFile(t, keyPEM)

	if _, err := Server(certFile, keyFile, filepath.Join(t.TempDir(), "missing-ca.crt")); err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestServerFailsOnMalformedCA(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)
	certFile := writeTempFile(t, certPEM)
	keyFile := writeTempFile(t, keyPEM)
	badCAFile := writeTempFile(t, []byte("not a valid pem"))

	if _, err := Server(certFile, keyFile, badCAFile); err == nil {
		t.Fatal("expected error for malformed CA file")
	}
}
