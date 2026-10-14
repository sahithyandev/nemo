#!/bin/sh
# installs the latest nemo release for the current OS/arch. no Go required.
# usage: curl -fsSL https://raw.githubusercontent.com/sahithyandev/nemo/main/scripts/install.sh | sh
set -eu

repo="sahithyandev/nemo"

os=$(uname -s)
case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*)
		echo "unsupported OS: $os (nemo ships Linux and macOS binaries; Windows users run scripts/install.ps1)" >&2
		exit 1
		;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*)
		echo "unsupported architecture: $arch" >&2
		exit 1
		;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
	grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
version=${tag#v}
archive="nemo_${version}_${os}_${arch}.tar.gz"
url="https://github.com/$repo/releases/download/$tag/$archive"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

echo "downloading $url"
curl -fsSL "$url" -o "$tmpdir/$archive"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"

bindir="/usr/local/bin"
if [ ! -w "$bindir" ]; then
	bindir="$HOME/.local/bin"
	mkdir -p "$bindir"
fi

mv "$tmpdir/nemo" "$bindir/nemo"
chmod +x "$bindir/nemo"
echo "installed nemo $version to $bindir/nemo"

case ":$PATH:" in
	*":$bindir:"*) ;;
	*) echo "add $bindir to your PATH to run 'nemo' directly" ;;
esac

# zsh completion: save the script once and source it from ~/.zshrc
case "${SHELL:-}" in
	*/zsh)
		mkdir -p "$HOME/.nemo"
		"$bindir/nemo" completion zsh >"$HOME/.nemo/_nemo"
		line='[ -s ~/.nemo/_nemo ] && source ~/.nemo/_nemo'
		if ! grep -qsF "$line" "$HOME/.zshrc"; then
			echo "$line" >>"$HOME/.zshrc"
		fi
		echo "zsh completion installed, open a new terminal to use it"
		;;
esac
