#!/bin/sh
# Generates the local dev mTLS certs (if not already present) and loads them
# into the go-event-platform namespace as Kubernetes Secrets, one per
# service that terminates/originates TLS. Safe to re-run: cert generation is
# idempotent and `kubectl apply` on the rendered Secret is a no-op when the
# contents match.
#
# Requires: ./certs/generate.sh's output (regenerated here if missing),
# kubectl pointed at the target cluster, and the go-event-platform
# namespace already applied (see k8s/00-namespace.yaml).
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")
CERTS_DIR="$REPO_ROOT/certs"
NAMESPACE=go-event-platform

if [ ! -f "$CERTS_DIR/ca.crt" ]; then
	sh "$CERTS_DIR/generate.sh"
fi

create_secret() {
	name=$1
	service=$2
	kubectl create secret generic "$name" \
		-n "$NAMESPACE" \
		--from-file=ca.crt="$CERTS_DIR/ca.crt" \
		--from-file=tls.crt="$CERTS_DIR/$service.crt" \
		--from-file=tls.key="$CERTS_DIR/$service.key" \
		--dry-run=client -o yaml | kubectl apply -f -
}

create_secret inventory-service-tls inventory-service
create_secret order-service-tls order-service

echo "TLS secrets applied in namespace $NAMESPACE"
