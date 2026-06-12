#!/bin/bash
set -euo pipefail

VERSION="${1:-latest}"
BASE_IMAGE="darkver8/cosmo-nps"
NODE_IMAGE="darkver8/cosmo-nps-node"
CLIENT_IMAGE="darkver8/cosmo-nps-client"

echo "========================================="
echo "  Cosmo NPS image build & push"
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

echo "[1/4] Building ${BASE_IMAGE}:${VERSION} ..."
docker build -t "${BASE_IMAGE}:${VERSION}" .

echo "[2/4] Tagging role images ..."
docker tag "${BASE_IMAGE}:${VERSION}" "${NODE_IMAGE}:${VERSION}"
docker tag "${BASE_IMAGE}:${VERSION}" "${CLIENT_IMAGE}:${VERSION}"

if [[ "$VERSION" != "latest" ]]; then
    docker tag "${BASE_IMAGE}:${VERSION}" "${BASE_IMAGE}:latest"
    docker tag "${BASE_IMAGE}:${VERSION}" "${NODE_IMAGE}:latest"
    docker tag "${BASE_IMAGE}:${VERSION}" "${CLIENT_IMAGE}:latest"
fi

echo "[3/4] Pushing ${VERSION} tags ..."
docker push "${BASE_IMAGE}:${VERSION}"
docker push "${NODE_IMAGE}:${VERSION}"
docker push "${CLIENT_IMAGE}:${VERSION}"

if [[ "$VERSION" != "latest" ]]; then
    echo "[4/4] Pushing latest tags ..."
    docker push "${BASE_IMAGE}:latest"
    docker push "${NODE_IMAGE}:latest"
    docker push "${CLIENT_IMAGE}:latest"
else
    echo "[4/4] latest tags already pushed."
fi

echo ""
echo "Done:"
echo "  ${BASE_IMAGE}:${VERSION}"
echo "  ${NODE_IMAGE}:${VERSION}"
echo "  ${CLIENT_IMAGE}:${VERSION}"
