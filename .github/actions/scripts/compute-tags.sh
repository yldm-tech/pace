#!/usr/bin/env bash
#
# Single source of truth for this fork's image tag policy.
#
# Both build-push and merge-manifest call this, so a per-arch build and the manifest merge that tags it can never disagree about what the tag should be. If this logic is ever duplicated instead, drift silently publishes images under tags that the release notes and compose files do not reference.
#
# Reads configuration from the environment:
#   IMG_OWNER      required, the registry-qualified owner, e.g. ghcr.io/yldm-tech
#   IMG_NAME       required, e.g. plane-backend
#   BUILD_RELEASE  "true" selects the release tag regime
#   IS_PRERELEASE  "true" suppresses the :stable tag on a release
#   REL_VERSION    semver, required when BUILD_RELEASE=true
#   TARGET_BRANCH  github.ref_name; "master" selects the :latest regime
#
# Writes DOCKER_TAGS to both $GITHUB_ENV and $GITHUB_OUTPUT, and echoes it for the job log.

set -euo pipefail

: "${IMG_OWNER:?IMG_OWNER is required}"
: "${IMG_NAME:?IMG_NAME is required}"

BUILD_RELEASE="${BUILD_RELEASE:-false}"
IS_PRERELEASE="${IS_PRERELEASE:-false}"
REL_VERSION="${REL_VERSION:-latest}"
TARGET_BRANCH="${TARGET_BRANCH:-}"

# Strip anything that is not legal in a docker tag. Branch names routinely carry slashes (feat/PAI-123-thing), which would otherwise be read as a registry path separator.
FLAT_BRANCH_VERSION=$(printf '%s' "$TARGET_BRANCH" | sed 's/[^a-zA-Z0-9.-]//g')

if [ "$BUILD_RELEASE" == "true" ]; then
  SEMVER_REGEX="^v([0-9]+)\.([0-9]+)\.([0-9]+)(-[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*)?$"
  if [[ ! $REL_VERSION =~ $SEMVER_REGEX ]]; then
    echo "Invalid Release Version Format : ${REL_VERSION}" >&2
    echo "Please provide a valid SemVer version" >&2
    echo "e.g. v1.2.3 or v1.2.3-alpha-1" >&2
    echo "Exiting the build process" >&2
    exit 1
  fi
fi

if [ "$BUILD_RELEASE" == "true" ]; then
  DOCKER_TAGS="${IMG_OWNER}/${IMG_NAME}:${REL_VERSION}"
  if [ "$IS_PRERELEASE" != "true" ]; then
    DOCKER_TAGS="${DOCKER_TAGS},${IMG_OWNER}/${IMG_NAME}:stable"
  fi
elif [ "$TARGET_BRANCH" == "master" ]; then
  DOCKER_TAGS="${IMG_OWNER}/${IMG_NAME}:latest"
else
  DOCKER_TAGS="${IMG_OWNER}/${IMG_NAME}:${FLAT_BRANCH_VERSION}"
fi

echo "DOCKER_TAGS=${DOCKER_TAGS}"
if [ -n "${GITHUB_ENV:-}" ]; then
  echo "DOCKER_TAGS=${DOCKER_TAGS}" >>"$GITHUB_ENV"
fi
if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "DOCKER_TAGS=${DOCKER_TAGS}" >>"$GITHUB_OUTPUT"
fi
