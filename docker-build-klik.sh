echo "Start build and Run"

VERSION="$(git describe --tags --always --dirty)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

docker build \
    --build-arg COMMIT="${COMMIT}" \
    --build-arg VERSION="${VERSION}" \
    --build-arg BUILD_DATE="${BUILD_DATE}" \
    -t cliproxyapi:local-forowl .

export CLI_PROXY_IMAGE="cliproxyapi:local-forowl"
echo "Starting the services..."
docker compose up -d --remove-orphans --pull never

echo "Build complete. Services are starting."
echo "Run 'docker compose logs -f' to see the logs."
