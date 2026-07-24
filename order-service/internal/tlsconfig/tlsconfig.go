package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Client builds a *tls.Config for the gRPC client: certFile/keyFile are this
// service's own identity, presented to inventory-service for mutual TLS, and
// caFile is used to verify inventory-service's server certificate.
func Client(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert/key: %w", err)
	}

	pool, err := loadCA(caFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}, nil
}

func loadCA(caFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("parse ca cert %q: no valid certificates found", caFile)
	}

	return pool, nil
}
