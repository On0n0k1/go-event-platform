package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Server builds a *tls.Config for the gRPC server: certFile/keyFile are the
// server's own identity, and caFile is used to require and verify a client
// certificate on every connection (mutual TLS) -- order-service is the only
// intended caller, so an unauthenticated or wrongly-signed client is
// rejected at the handshake rather than at the application layer.
func Server(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert/key: %w", err)
	}

	pool, err := loadCA(caFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
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
