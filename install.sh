#!/bin/sh
# Installs the latest ape release to ~/.local/bin and puts that on PATH for your shell.
#
#   curl -fsSL https://raw.githubusercontent.com/AaronCQL/ape/main/install.sh | sh
#
# APE_NO_MODIFY_PATH=1   leave shell config alone
# APE_RELEASE_URL=…      download from elsewhere, e.g. https://github.com/AaronCQL/ape/releases/download/v0.5.0
set -eu

url=${APE_RELEASE_URL:-https://github.com/AaronCQL/ape/releases/latest/download}
dir=$HOME/.local/bin

die() {
	echo "ape install: $*" >&2
	exit 1
}

os=$(uname -s)
arch=$(uname -m)
case $os-$arch in
Linux-x86_64 | Linux-aarch64 | Darwin-x86_64 | Darwin-arm64) ;;
Linux-arm64) arch=aarch64 ;;
Linux-* | Darwin-*) die "no build for $arch; build from source: go install github.com/AaronCQL/ape@latest" ;;
*) die "ape runs on Linux and macOS, not $os" ;;
esac

# Download beside the target, check it runs, then rename: a failed install never leaves a broken ape.
mkdir -p "$dir"
tmp=$(mktemp "$dir/.ape.XXXXXX")
trap 'rm -f "$tmp"' EXIT
curl -fsSL "$url/ape-$os-$arch" -o "$tmp" || die "download failed: $url/ape-$os-$arch"
chmod +x "$tmp"
version=$("$tmp" version) || die "the downloaded binary doesn't run"
mv -f "$tmp" "$dir/ape"
echo "installed $version to $dir/ape"

case ":$PATH:" in
*":$dir:"*)
	echo "next: cd into your repo and run: ape init"
	exit 0
	;;
esac

line='export PATH="$HOME/.local/bin:$PATH"'
case ${SHELL##*/} in
zsh) rc=${ZDOTDIR:-$HOME}/.zshrc ;;
bash)
	# Linux terminals start bash interactive (.bashrc); macOS Terminal starts it as a login shell, which reads the
	# first of these that exists, so writing a new .bash_profile would hide an existing .profile.
	rc=$HOME/.bashrc
	if [ "$os" = Darwin ]; then
		rc=$HOME/.bash_profile
		for f in "$HOME/.bash_profile" "$HOME/.bash_login" "$HOME/.profile"; do
			if [ -f "$f" ]; then
				rc=$f
				break
			fi
		done
	fi
	;;
fish)
	rc=${XDG_CONFIG_HOME:-$HOME/.config}/fish/conf.d/ape.fish
	line='contains $HOME/.local/bin $PATH; or set -gx PATH $HOME/.local/bin $PATH'
	;;
*) rc=$HOME/.profile ;;
esac

if [ "${APE_NO_MODIFY_PATH:-}" = 1 ]; then
	echo "$dir isn't on your PATH; add this to $rc: $line"
elif grep -qsF "$line" "$rc"; then
	echo "$rc already puts $dir on PATH; open a new terminal"
else
	mkdir -p "$(dirname "$rc")"
	printf '\n# ape\n%s\n' "$line" >>"$rc"
	echo "added $dir to PATH in $rc; open a new terminal, or run: $line"
fi
echo "next: cd into your repo and run: ape init"
