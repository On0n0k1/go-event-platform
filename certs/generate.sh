#!/bin/sh
# Generates a throwaway self-signed CA plus a server cert (inventory-service)
# and client cert (order-service) for the order-service <-> inventory-service
# gRPC mTLS connection. Local-dev only -- never use these for anything real.
#
# Run automatically by the `certs` service in docker-compose.yml before
# inventory-service/order-service start; safe to re-run (skips if certs
# already exist).
set -eu

apk add --no-cache openssl >/dev/null 2>&1

CERT_DIR=$(dirname "$0")
cd "$CERT_DIR"

if [ -f ca.crt ]; then
	echo "certs already exist, skipping generation"
	exit 0
fi

openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
	-keyout ca.key -out ca.crt -subj "/CN=go-event-platform-dev-ca"

gen_cert() {
	name=$1
	openssl req -newkey rsa:2048 -nodes \
		-keyout "$name.key" -out "$name.csr" -subj "/CN=$name"

	printf "subjectAltName=DNS:%s,DNS:localhost\n" "$name" > "$name.ext"

	openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
		-out "$name.crt" -days 3650 -extfile "$name.ext"
}

gen_cert inventory-service
gen_cert order-service

rm -f ./*.csr ./*.ext ./*.srl
chmod 644 ./*.crt ./*.key

echo "certs generated"
