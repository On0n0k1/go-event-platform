#!/bin/sh
# Starts or stops the full go-event-platform stack on a local kind cluster via
# Helm. Mirrors the "Running on Kubernetes" / "Packaging as Helm" sections of
# the README as a single command instead of copy-pasting each step by hand.
#
# Usage:
#   k8s/run.sh up      # create the cluster (if needed) and deploy everything
#   k8s/run.sh down     # uninstall the release and delete the cluster
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO_ROOT=$(dirname "$SCRIPT_DIR")
CLUSTER_NAME=go-event-platform
NAMESPACE=go-event-platform
RELEASE=go-event-platform

services="api-gateway order-service inventory-service notification-service analytics-service"

up() {
	if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
		echo "kind cluster '$CLUSTER_NAME' already exists, reusing it"
	else
		echo "==> creating kind cluster"
		kind create cluster --config "$REPO_ROOT/kind-config.yaml"
	fi

	echo "==> applying namespace"
	kubectl apply -f "$REPO_ROOT/k8s/00-namespace.yaml"

	if ! kubectl get deployment ingress-nginx-controller -n ingress-nginx >/dev/null 2>&1; then
		echo "==> installing ingress-nginx"
		kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml
	fi
	echo "==> waiting for ingress-nginx controller"
	for i in 1 2 3 4 5; do
		if kubectl wait --namespace ingress-nginx --for=condition=ready pod \
			--selector=app.kubernetes.io/component=controller --timeout=120s 2>/dev/null; then
			break
		fi
		[ "$i" -eq 5 ] && { echo "ingress-nginx controller never became ready" >&2; exit 1; }
		sleep 3
	done

	echo "==> building and loading service images"
	for svc in $services; do
		docker build -t "$svc:local" "$REPO_ROOT/$svc"
	done
	# shellcheck disable=SC2086
	kind load docker-image $(for svc in $services; do printf '%s:local ' "$svc"; done) --name "$CLUSTER_NAME"

	echo "==> generating TLS secrets"
	"$REPO_ROOT/k8s/generate-secrets.sh"

	release_existed=false
	if helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1; then
		release_existed=true
	fi

	echo "==> deploying via helm"
	helm upgrade --install "$RELEASE" "$REPO_ROOT/helm/go-event-platform" \
		--namespace "$NAMESPACE" --create-namespace

	# The image tag (:local) never changes, so on a rerun against an already
	# -existing release, helm sees no Deployment spec diff for the app
	# services and won't restart them -- force it, so a rebuilt image
	# actually takes effect. Skipped on a brand new install: restarting a
	# Deployment whose first-ever rollout hasn't finished yet races the wait
	# below. Only the 5 app services (built from local Dockerfiles above),
	# not the infra deployments, which don't get rebuilt between reruns.
	if [ "$release_existed" = true ]; then
		echo "==> restarting app deployments to pick up any rebuilt images"
		# shellcheck disable=SC2086
		kubectl rollout restart $(for svc in $services; do printf 'deployment/%s ' "$svc"; done) -n "$NAMESPACE"
	fi

	# Deployment-name-scoped rollout status, not `kubectl wait pod --all`:
	# the latter snapshots pod *names* up front, which races pods being
	# replaced by the restart above (the old name 404s mid-wait instead of
	# being tracked through to its replacement).
	echo "==> waiting for rollouts"
	for dep in $(kubectl get deployments -n "$NAMESPACE" -o name); do
		kubectl rollout status "$dep" -n "$NAMESPACE" --timeout=180s
	done

	echo
	echo "Up. Open http://localhost:8090"
}

down() {
	if helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1; then
		echo "==> uninstalling helm release"
		helm uninstall "$RELEASE" -n "$NAMESPACE"
	fi
	if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
		echo "==> deleting kind cluster"
		kind delete cluster --name "$CLUSTER_NAME"
	fi
	echo "Down."
}

case "${1:-}" in
up) up ;;
down) down ;;
*)
	echo "usage: $0 {up|down}" >&2
	exit 1
	;;
esac
