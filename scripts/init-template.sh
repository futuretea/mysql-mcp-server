#!/usr/bin/env bash

set -euo pipefail

readonly MODULE_PLACEHOLDER='example.invalid/mcp-template-module-placeholder'
readonly BINARY_PLACEHOLDER='mcp-template-binary-placeholder'
readonly NPM_PLACEHOLDER='mcp-template-npm-package-placeholder'
readonly IMAGE_PLACEHOLDER='mcp-template-image-placeholder'

module=''
binary=''
npm_package=''
image=''
with_release=false
dry_run=false

usage() {
  cat <<'EOF'
Initialize a repository created from this template.

Usage:
  ./scripts/init-template.sh --module <path> --binary <name> \
    --npm-package <name> --image <registry/repository> [--with-release] [--dry-run]

Required flags:
  --module <path>         Go module path, for example github.com/acme/my-mcp
  --binary <name>         Lowercase binary name, for example my-mcp
  --npm-package <name>    npm package name, for example @acme/my-mcp
  --image <image>         Fully qualified Docker image, for example ghcr.io/acme/my-mcp

Optional flags:
  --with-release          Install npm and Docker release workflows and package scaffolding
  --dry-run               Print the planned changes without modifying files
  -h, --help              Show this help message

The script is non-interactive and can be run once on an unmodified template copy.
EOF
}

fail() {
  printf 'error: %s\n' "$1" >&2
  exit 2
}

require_value() {
  [ "$#" -ge 2 ] && [ -n "$2" ] || fail "$1 requires a value"
  printf '%s' "$2"
}

matches() {
  printf '%s\n' "$1" | LC_ALL=C grep -Eq "$2"
}

require_marker() {
  grep -Fq "$1" "$2" || fail "expected placeholder $1 in $2; this template may already be initialized"
}

require_absent() {
  [ ! -e "$1" ] || fail "refusing to overwrite existing $1"
}

replace_files() {
  local source_value=$1
  local target_value=$2
  shift 2
  SOURCE_VALUE="$source_value" TARGET_VALUE="$target_value" \
    perl -pi -e 's/\Q$ENV{SOURCE_VALUE}\E/$ENV{TARGET_VALUE}/g' "$@"
}

replace_go_sources() {
  local source_value=$1
  local target_value=$2
  local file=''
  while IFS= read -r file; do
    replace_files "$source_value" "$target_value" "$file"
  done < <(find cmd internal pkg -type f -name '*.go' -print)
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --module)
      module=$(require_value "$@")
      shift 2
      ;;
    --binary)
      binary=$(require_value "$@")
      shift 2
      ;;
    --npm-package)
      npm_package=$(require_value "$@")
      shift 2
      ;;
    --image)
      image=$(require_value "$@")
      shift 2
      ;;
    --with-release)
      with_release=true
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ -n "$module" ] || fail '--module is required; see --help'
[ -n "$binary" ] || fail '--binary is required; see --help'
[ -n "$npm_package" ] || fail '--npm-package is required; see --help'
[ -n "$image" ] || fail '--image is required; see --help'

matches "$module" '^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)+$' \
  || fail '--module must be a slash-separated module path'
matches "$binary" '^[a-z0-9][a-z0-9._-]*$' \
  || fail '--binary must contain only lowercase letters, digits, dots, underscores, or hyphens'
matches "$npm_package" '^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$' \
  || fail '--npm-package must be an unscoped or scoped lowercase npm package name'
matches "$image" '^[a-z0-9][a-z0-9._:-]*(/[a-z0-9][a-z0-9._-]*)+$' \
  || fail '--image must be a lowercase Docker image without a tag'

registry=${image%%/*}
case "$registry" in
  *.*|*:*|localhost) ;;
  *) fail '--image must include a registry hostname, for example ghcr.io/acme/my-mcp' ;;
esac

if [ "$with_release" = true ]; then
  case "${binary%%.*}" in
    con|prn|aux|nul|com[1-9]|lpt[1-9])
      fail '--binary must not use a Windows reserved device name with --with-release'
      ;;
  esac
fi

npm_directory=$(printf '%s' "$npm_package" | sed 's/^@//; s/\//-/g')
template_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$template_root"

require_marker "$MODULE_PLACEHOLDER" go.mod
require_marker "$BINARY_PLACEHOLDER" Makefile
require_marker "$IMAGE_PLACEHOLDER" Makefile
require_marker "$BINARY_PLACEHOLDER" Dockerfile
require_absent .github/workflows/build.yaml

go_version=$(awk '$1 == "go" { print $2; exit }' go.mod)
[ -n "$go_version" ] || fail 'could not read the Go version from go.mod'

if [ "$with_release" = true ]; then
  require_absent .github/workflows/release-npm.yaml
  require_absent .github/workflows/release-docker.yaml
  require_absent .github/ci
  require_absent npm
  require_absent release.mk
  require_marker "$NPM_PLACEHOLDER" templates/npm/package/package.json
  require_marker "$IMAGE_PLACEHOLDER" templates/ci/release-docker.yaml
fi

if [ "$dry_run" = true ]; then
  printf 'Would initialize this repository with:\n'
  printf '  module: %s\n  binary: %s\n  npm package: %s\n  image: %s\n' \
    "$module" "$binary" "$npm_package" "$image"
  printf 'Would create .github/workflows/build.yaml\n'
  if [ "$with_release" = true ]; then
    printf '%s\n' 'Would create release workflows, release.mk, npm/, .github/ci/release-platforms.json, and append release usage documentation to both READMEs'
  fi
  exit 0
fi

mkdir -p .github/workflows
cp templates/ci/build.yaml .github/workflows/build.yaml

replace_files "$MODULE_PLACEHOLDER" "$module" go.mod Makefile Dockerfile README.md README.zh.md
replace_go_sources "$MODULE_PLACEHOLDER" "$module"
replace_files "$BINARY_PLACEHOLDER" "$binary" Makefile Dockerfile README.md README.zh.md .github/workflows/build.yaml
replace_go_sources "$BINARY_PLACEHOLDER" "$binary"
replace_files "$IMAGE_PLACEHOLDER" "$image" Makefile README.md README.zh.md
replace_files '__MCP_GO_VERSION__' "$go_version" .github/workflows/build.yaml

if [ "$with_release" = true ]; then
  mkdir -p .github/ci
  cp templates/ci/release-npm.yaml .github/workflows/release-npm.yaml
  cp templates/ci/release-docker.yaml .github/workflows/release-docker.yaml
  cp templates/ci/release-platforms.json .github/ci/release-platforms.json
  cp templates/release.mk release.mk
  cp -R templates/npm npm

  for package_dir in npm/platform-*; do
    platform=${package_dir#npm/platform-}
    mv "$package_dir" "npm/$npm_directory-$platform"
  done
  mv npm/package "npm/$npm_directory"

  release_files=(.github/workflows/release-npm.yaml .github/workflows/release-docker.yaml release.mk)
  while IFS= read -r package_file; do
    release_files+=("$package_file")
  done < <(find npm -type f -print)

  replace_files "$MODULE_PLACEHOLDER" "$module" "${release_files[@]}"
  replace_files "$BINARY_PLACEHOLDER" "$binary" "${release_files[@]}"
  replace_files "$NPM_PLACEHOLDER" "$npm_package" "${release_files[@]}"
  replace_files '__MCP_NPM_DIRECTORY__' "$npm_directory" "${release_files[@]}"
  replace_files "$IMAGE_PLACEHOLDER" "$image" "${release_files[@]}"
  replace_files '__MCP_DOCKER_REGISTRY__' "$registry" "${release_files[@]}"
  replace_files '__MCP_GO_VERSION__' "$go_version" "${release_files[@]}"
  replace_files '# __MCP_RELEASE_INCLUDE__' '-include release.mk' Makefile
  cat templates/docs/release-usage.md >> README.md
  cat templates/docs/release-usage.zh.md >> README.zh.md
else
  replace_files '# __MCP_RELEASE_INCLUDE__' '' Makefile
fi

printf 'Initialized %s. Review the generated files before committing.\n' "$module"
