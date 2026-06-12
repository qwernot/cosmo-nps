#!/bin/bash
set -euo pipefail

VERSION="${1:-latest}"

CONTROL_IMAGE="darkver8/cosmo-nps"
NODE_IMAGE="darkver8/cosmo-nps-node"
CLIENT_IMAGE="darkver8/cosmo-nps-client"

echo "========================================="
echo "  Cosmo NPS role image build & push"
echo "  Version: ${VERSION}"
echo "========================================="
echo ""

if [[ ! -f "Dockerfile" ]]; then
    echo "[ERROR] Dockerfile not found. Run this script from the project root."
    exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
    echo "[ERROR] Docker is not installed."
    exit 1
fi

echo "[1/6] Building control image ..."
docker build --target control -t "${CONTROL_IMAGE}:${VERSION}" .

echo "[2/6] Building node image ..."
docker build --target node -t "${NODE_IMAGE}:${VERSION}" .

echo "[3/6] Building client image ..."
docker build --target client -t "${CLIENT_IMAGE}:${VERSION}" .

if [[ "$VERSION" != "latest" ]]; then
    docker tag "${CONTROL_IMAGE}:${VERSION}" "${CONTROL_IMAGE}:latest"
    docker tag "${NODE_IMAGE}:${VERSION}" "${NODE_IMAGE}:latest"
    docker tag "${CLIENT_IMAGE}:${VERSION}" "${CLIENT_IMAGE}:latest"
fi

echo "[4/6] Pushing control image ..."
docker push "${CONTROL_IMAGE}:${VERSION}"

echo "[5/6] Pushing node image ..."
docker push "${NODE_IMAGE}:${VERSION}"

echo "[6/6] Pushing client image ..."
docker push "${CLIENT_IMAGE}:${VERSION}"

if [[ "$VERSION" != "latest" ]]; then
    docker push "${CONTROL_IMAGE}:latest"
    docker push "${NODE_IMAGE}:latest"
    docker push "${CLIENT_IMAGE}:latest"
fi

echo ""
echo "Done:"
echo "  ${CONTROL_IMAGE}:${VERSION}"
echo "  ${NODE_IMAGE}:${VERSION}"
echo "  ${CLIENT_IMAGE}:${VERSION}"
