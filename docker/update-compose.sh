#!/usr/bin/bash
set -e

usage() {
	echo "Usage: $0 -p <project> -s <service> -t <tag>"
	echo "Example: $0 -p devbox -s api -t latest"
	echo ""
	echo "  -p <project>   Project name (folder name in $HOME)"
	echo "  -s <service>   Service to update (matches compose service name)"
	echo "  -t <tag>       Tag to use for the image"
	echo "  -h             Show this help message"
	exit 1
}

while getopts "p:s:t:h" opt; do
	case $opt in
		p) ARG_PROJECT=$OPTARG ;;
		s) ARG_SERVICE=$OPTARG ;;
		t) ARG_TAG=$OPTARG ;;
		h) usage ;;
		*) usage ;;
	esac
done

# Validation
if [ -z "$ARG_PROJECT" ] || [ -z "$ARG_SERVICE" ]; then
	echo "ERROR: Project (-p) and Service (-s) are required."
	usage
fi

PROJECT=$ARG_PROJECT
SERVICE=$ARG_SERVICE
TAG=${ARG_TAG:-latest}

PROJECT_DIR="$HOME/$PROJECT"
DOCKER_DIR="$PROJECT_DIR/docker"
LOCAL_IMAGE="$PROJECT-$SERVICE:latest"
TAG_IMAGE="wwwill-1.lab:5000/$PROJECT/$SERVICE:$TAG"
BUILD="Dockerfile.$SERVICE"

# Build the new image
docker build -f "$DOCKER_DIR/build/$BUILD" -t "$LOCAL_IMAGE" "$PROJECT_DIR"

# Tag and Push to registry
docker tag "$LOCAL_IMAGE" "$TAG_IMAGE"
docker push "$TAG_IMAGE"

# Start/Update container via Compose
cd "$DOCKER_DIR"
docker compose up -d "$SERVICE"

# Clean up local build image
docker rmi "$LOCAL_IMAGE"