#!/bin/sh
# Install the latest yagit release binary for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/install.sh | sh
#
# Optional environment:
#   YAGIT_VERSION      release tag (default: latest)
#   YAGIT_INSTALL_DIR  directory for the launcher (default: ~/.local/bin)
#   YAGIT_HOME         directory for the binary (default: ~/.local/share/yagit)

set -eu

REPO="ngsanogo/yagit"
BASE_URL="https://github.com/${REPO}/releases"
INSTALL_DIR="${YAGIT_INSTALL_DIR:-${HOME}/.local/bin}"
YAGIT_HOME="${YAGIT_HOME:-${HOME}/.local/share/yagit}"

say() {
	printf '%s\n' "$*"
}

die() {
	printf 'yagit install: %s\n' "$*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || die "need ${1} on PATH"
}

detect_target() {
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	arch="$(uname -m)"

	case "$os" in
	linux) os=linux ;;
	darwin) os=darwin ;;
	mingw* | msys* | cygwin*)
		die "use the PowerShell installer on Windows: irm https://raw.githubusercontent.com/${REPO}/main/scripts/install.ps1 | iex"
		;;
	*) die "unsupported OS: $(uname -s)" ;;
	esac

	case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture: $(uname -m)" ;;
	esac

	printf '%s-%s' "$os" "$arch"
}

download() {
	url=$1
	out=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --proto '=https' --tlsv1.2 -o "$out" "$url" \
			|| die "download failed: ${url}"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$out" "$url" || die "download failed: ${url}"
	else
		die "need curl or wget on PATH"
	fi
}

checksum_ok() {
	file=$1
	expected=$2
	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$file" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		got=$(shasum -a 256 "$file" | awk '{print $1}')
	else
		die "need sha256sum or shasum to verify the download"
	fi
	[ "$got" = "$expected" ] || die "checksum mismatch for $(basename "$file") (got ${got}, want ${expected})"
}

write_launcher() {
	launcher=$1
	binary=$2
	cat >"$launcher" <<EOF
#!/bin/sh
# Launcher installed by scripts/install.sh — defaults a repository root and a
# session token file so \`yagit\` is enough to start.
set -eu
ROOT="\${YAGIT_ROOT:-\${HOME}}"
TOKEN_FILE="\${YAGIT_TOKEN_FILE:-\${XDG_STATE_HOME:-\${HOME}/.local/state}/yagit/token}"
mkdir -p "\$(dirname "\$TOKEN_FILE")"
exec "${binary}" -root "\$ROOT" -token-file "\$TOKEN_FILE" "\$@"
EOF
	chmod 755 "$launcher"
}

main() {
	need uname
	target=$(detect_target)
	asset="yagit-${target}"

	if [ -n "${YAGIT_VERSION:-}" ]; then
		version="$YAGIT_VERSION"
		case "$version" in
		v*) ;;
		*) version="v${version}" ;;
		esac
		download_root="${BASE_URL}/download/${version}"
	else
		version=latest
		download_root="${BASE_URL}/latest/download"
	fi

	tmpdir=$(mktemp -d)
	# shellcheck disable=SC2064
	trap 'rm -rf "$tmpdir"' EXIT INT HUP TERM

	say "Downloading yagit (${version}, ${target})…"
	download "${download_root}/${asset}" "${tmpdir}/${asset}"
	download "${download_root}/SHA256SUMS" "${tmpdir}/SHA256SUMS"

	expected=$(awk -v name="$asset" '$2 == name { print $1; exit }' "${tmpdir}/SHA256SUMS")
	[ -n "$expected" ] || die "SHA256SUMS has no entry for ${asset}"
	checksum_ok "${tmpdir}/${asset}" "$expected"

	mkdir -p "$YAGIT_HOME" "$INSTALL_DIR"
	binary="${YAGIT_HOME}/yagit"
	mv "${tmpdir}/${asset}" "$binary"
	chmod 755 "$binary"

	launcher="${INSTALL_DIR}/yagit"
	write_launcher "$launcher" "$binary"

	say
	say "Installed:"
	say "  binary   ${binary}"
	say "  command  ${launcher}"
	say
	case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*)
		say "Add ${INSTALL_DIR} to your PATH, then run:"
		say
		;;
	esac
	say "  yagit"
	say
	say "Open the URL it prints. Repositories under \$HOME are allowed by default;"
	say "set YAGIT_ROOT to narrow that. Requires git on PATH."
}

main "$@"
