#!/bin/sh
# Remove a yagit release install made by scripts/install.sh.
#
#   curl -fsSL https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/uninstall.sh | sh
#
# Optional environment (same defaults as the installer):
#   YAGIT_INSTALL_DIR  directory holding the launcher (default: ~/.local/bin)
#   YAGIT_HOME         directory holding the binary (default: ~/.local/share/yagit)

set -eu

INSTALL_DIR="${YAGIT_INSTALL_DIR:-${HOME}/.local/bin}"
YAGIT_HOME="${YAGIT_HOME:-${HOME}/.local/share/yagit}"
TOKEN_FILE="${YAGIT_TOKEN_FILE:-${XDG_STATE_HOME:-${HOME}/.local/state}/yagit/token}"

say() {
	printf '%s\n' "$*"
}

die() {
	printf 'yagit uninstall: %s\n' "$*" >&2
	exit 1
}

remove_path() {
	path=$1
	kind=$2
	if [ ! -e "$path" ] && [ ! -L "$path" ]; then
		say "  ${kind}  absent (${path})"
		return 0
	fi
	# Refuse to follow a symlink into somebody else's tree: remove the link,
	# never what it points at. A directory is only removed when it is exactly
	# YAGIT_HOME and empty after the binary goes — never a recursive wipe of
	# a path the user may have pointed elsewhere.
	if [ -L "$path" ]; then
		rm -f "$path" || die "cannot remove ${kind}: ${path}"
	elif [ -f "$path" ]; then
		rm -f "$path" || die "cannot remove ${kind}: ${path}"
	elif [ -d "$path" ]; then
		rmdir "$path" 2>/dev/null || die "cannot remove ${kind}: ${path} is not empty"
	else
		die "${kind} is neither a file nor a directory: ${path}"
	fi
	say "  ${kind}  removed (${path})"
}

main() {
	launcher="${INSTALL_DIR}/yagit"
	binary="${YAGIT_HOME}/yagit"

	say "Uninstalling yagit…"
	remove_path "$launcher" "command"
	remove_path "$binary" "binary"
	# The home directory holds only the binary the installer put there. If it
	# is empty afterwards, take it too — leaving an empty share directory is
	# noise. If anything else landed in it, leave it and say so.
	if [ -d "$YAGIT_HOME" ]; then
		if rmdir "$YAGIT_HOME" 2>/dev/null; then
			say "  home     removed (${YAGIT_HOME})"
		else
			say "  home     kept (${YAGIT_HOME} is not empty)"
		fi
	fi

	say
	if [ -e "$TOKEN_FILE" ] || [ -L "$TOKEN_FILE" ]; then
		say "Session token left at ${TOKEN_FILE}."
		say "Remove it by hand if you no longer want that secret on disk:"
		say
		say "  rm -f ${TOKEN_FILE}"
		say
	else
		say "No session token found at ${TOKEN_FILE}."
		say
	fi
	say "Done."
}

main "$@"
