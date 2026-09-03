#!/bin/sh
# Install the latest qi release from GitHub.
#   curl -fsSL https://raw.githubusercontent.com/itsmostafa/qi/main/install.sh | sh
# Override the target directory with QI_INSTALL_DIR.
set -eu

REPO="itsmostafa/qi"
INSTALL_DIR="${QI_INSTALL_DIR:-$HOME/.local/bin}"

main() {
	os=$(uname -s | tr '[:upper:]' '[:lower:]')
	machine=$(uname -m)
	case "$machine" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) arch= ;;
	esac
	if { [ "$os" != darwin ] && [ "$os" != linux ]; } || [ -z "$arch" ]; then
		echo "unsupported platform: $os $machine (supported: darwin/linux on amd64/arm64)" >&2
		exit 1
	fi

	archive="qi-$os-$arch.tar.gz"
	base="https://github.com/$REPO/releases/latest/download"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT

	echo "Downloading $archive..."
	curl -fsSL -o "$tmp/$archive" "$base/$archive"
	curl -fsSL -o "$tmp/SHA256SUMS.txt" "$base/SHA256SUMS.txt"

	if command -v sha256sum >/dev/null 2>&1; then
		got=$(sha256sum "$tmp/$archive" | awk '{print $1}')
	else
		got=$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')
	fi
	want=$(awk -v f="$archive" '$2 == f {print $1}' "$tmp/SHA256SUMS.txt")
	if [ -z "$want" ]; then
		echo "no checksum for $archive in SHA256SUMS.txt" >&2
		exit 1
	fi
	if [ "$got" != "$want" ]; then
		echo "checksum mismatch: got $got, want $want" >&2
		exit 1
	fi

	tar -xzf "$tmp/$archive" -C "$tmp" qi
	chmod +x "$tmp/qi"
	mkdir -p "$INSTALL_DIR"
	mv "$tmp/qi" "$INSTALL_DIR/qi"

	echo "Installed to $INSTALL_DIR/qi"
	case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) echo "Add $INSTALL_DIR to your PATH to run qi from anywhere." >&2 ;;
	esac
	"$INSTALL_DIR/qi" --version
}

main "$@"
