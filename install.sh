#!/bin/sh
# Installs the forecast binary from the latest (or pinned) GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/commondatageek/delivery-forecast/main/install.sh | sh
#
# Supports Linux and macOS (including Apple Silicon under Rosetta). For
# native Windows, use install.ps1 instead.
#
# Config (environment variables):
#   FORECAST_INSTALL_DIR      install directory (default: $HOME/.forecast/bin)
#   FORECAST_VERSION          release tag to install, e.g. v1.2.3 (default: latest)
#   FORECAST_NO_MODIFY_PATH   set (to any value) to skip editing your shell profile
set -u

REPO="commondatageek/delivery-forecast"
BINARY="forecast"

say() {
    printf 'forecast-install: %s\n' "$1" >&2
}

warn() {
    printf 'forecast-install: warning: %s\n' "$1" >&2
}

err() {
    printf 'forecast-install: error: %s\n' "$1" >&2
    exit 1
}

check_cmd() {
    command -v "$1" >/dev/null 2>&1
}

# downloader URL OUTFILE — fetches URL into OUTFILE via curl, falling back to wget.
downloader() {
    if check_cmd curl; then
        curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
    elif check_cmd wget; then
        wget -q -O "$2" "$1"
    else
        err "need either 'curl' or 'wget' to download files"
    fi
}

# downloader_stdout URL — fetches URL and prints its body to stdout.
downloader_stdout() {
    if check_cmd curl; then
        curl -fsSL --proto '=https' --tlsv1.2 "$1"
    elif check_cmd wget; then
        wget -q -O - "$1"
    else
        err "need either 'curl' or 'wget' to download files"
    fi
}

detect_platform() {
    _ostype="$(uname -s)"
    _cputype="$(uname -m)"

    case "$_ostype" in
        Linux)
            _os=linux
            ;;
        Darwin)
            _os=darwin
            ;;
        *)
            err "unsupported OS '$_ostype' — for Windows, use install.ps1, or download a binary manually from https://github.com/$REPO/releases"
            ;;
    esac

    case "$_cputype" in
        x86_64 | amd64)
            _arch=amd64
            ;;
        aarch64 | arm64)
            _arch=arm64
            ;;
        *)
            err "unsupported CPU architecture '$_cputype'"
            ;;
    esac

    # Apple Silicon under Rosetta reports x86_64 via `uname -m`; detect the
    # real hardware so we still fetch the native arm64 build.
    if [ "$_os" = "darwin" ] && [ "$_arch" = "amd64" ]; then
        if [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = "1" ]; then
            _arch=arm64
        fi
    fi

    PLATFORM_OS="$_os"
    PLATFORM_ARCH="$_arch"
}

resolve_version() {
    if [ -n "${FORECAST_VERSION:-}" ]; then
        VERSION="$FORECAST_VERSION"
        return
    fi

    say "resolving latest release..."
    _api_url="https://api.github.com/repos/$REPO/releases/latest"
    _release_json="$(downloader_stdout "$_api_url")" || err "failed to fetch latest release info from $_api_url"

    VERSION="$(printf '%s' "$_release_json" | grep '"tag_name"' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"

    if [ -z "$VERSION" ]; then
        err "could not determine the latest release tag from $_api_url (does the repo have a published, non-prerelease release?)"
    fi
}

verify_checksum() {
    _file="$1"
    _name="$2"
    _checksums_file="$3"

    if check_cmd sha256sum; then
        _got="$(sha256sum "$_file" | awk '{print $1}')"
    elif check_cmd shasum; then
        _got="$(shasum -a 256 "$_file" | awk '{print $1}')"
    else
        err "need either 'sha256sum' or 'shasum' to verify the download"
    fi

    _want="$(grep -F "$_name" "$_checksums_file" | awk '{print $1}')"
    if [ -z "$_want" ]; then
        err "no checksum entry for $_name in checksums.txt"
    fi

    _got_lower="$(printf '%s' "$_got" | tr 'A-Z' 'a-z')"
    _want_lower="$(printf '%s' "$_want" | tr 'A-Z' 'a-z')"

    if [ "$_got_lower" != "$_want_lower" ]; then
        err "checksum mismatch for $_name: got $_got_lower, want $_want_lower"
    fi

    say "checksum OK"
}

detect_profile() {
    case "${SHELL:-}" in
        */zsh)
            if [ -f "$HOME/.zprofile" ]; then
                printf '%s' "$HOME/.zprofile"
            else
                printf '%s' "$HOME/.zshrc"
            fi
            ;;
        */bash)
            if [ -f "$HOME/.bash_profile" ]; then
                printf '%s' "$HOME/.bash_profile"
            else
                printf '%s' "$HOME/.bashrc"
            fi
            ;;
        *)
            printf '%s' "$HOME/.profile"
            ;;
    esac
}

maybe_modify_path() {
    _dir="$1"

    case ":$PATH:" in
        *":$_dir:"*)
            return
            ;;
    esac

    if [ -n "${FORECAST_NO_MODIFY_PATH:-}" ]; then
        say "note: $_dir is not on your PATH. Add it manually, or unset FORECAST_NO_MODIFY_PATH to have this script do it."
        return
    fi

    _profile="$(detect_profile)"
    if [ -z "$_profile" ]; then
        warn "could not determine your shell profile; add $_dir to your PATH manually"
        return
    fi

    _line="export PATH=\"$_dir:\$PATH\""

    if [ -f "$_profile" ] && grep -F "$_line" "$_profile" >/dev/null 2>&1; then
        say "PATH already configured in $_profile"
        return
    fi

    if ! printf '\n# added by forecast install.sh\n%s\n' "$_line" >> "$_profile"; then
        warn "failed to update $_profile; add $_dir to your PATH manually"
        return
    fi

    say "added $_dir to PATH in $_profile — restart your shell or run: . $_profile"
}

main() {
    detect_platform
    resolve_version

    INSTALL_DIR="${FORECAST_INSTALL_DIR:-$HOME/.forecast/bin}"

    _asset="${BINARY}_${VERSION}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
    _base_url="https://github.com/$REPO/releases/download/$VERSION"
    _asset_url="$_base_url/$_asset"
    _checksums_url="$_base_url/checksums.txt"

    _workdir="$(mktemp -d)" || err "failed to create a temp directory"
    trap 'rm -rf "$_workdir"' EXIT INT TERM

    say "downloading $_asset ($VERSION)..."
    downloader "$_asset_url" "$_workdir/$_asset" || err "failed to download $_asset_url"
    downloader "$_checksums_url" "$_workdir/checksums.txt" || err "failed to download $_checksums_url"

    verify_checksum "$_workdir/$_asset" "$_asset" "$_workdir/checksums.txt"

    say "extracting..."
    (cd "$_workdir" && tar -xzf "$_asset") || err "failed to extract $_asset"
    [ -f "$_workdir/$BINARY" ] || err "'$BINARY' not found inside $_asset"

    mkdir -p "$INSTALL_DIR" || err "failed to create install directory $INSTALL_DIR"
    mv "$_workdir/$BINARY" "$INSTALL_DIR/$BINARY" || err "failed to install to $INSTALL_DIR/$BINARY"
    chmod 755 "$INSTALL_DIR/$BINARY" || err "failed to chmod $INSTALL_DIR/$BINARY"

    maybe_modify_path "$INSTALL_DIR"

    say "installed $BINARY $VERSION to $INSTALL_DIR/$BINARY"
    "$INSTALL_DIR/$BINARY" version 2>/dev/null || true
}

main
