#!/bin/sh
# build.sh builds webshark's kiosk UI at the commit below into
# resources/webshark, which GoRex ships and serves to show packet
# captures (capture.go). It needs git and Node.js with npm. Files over
# 1 MB are kept gzipped: GoRex unpacks them as it serves them. A build of
# that commit already there is kept; -f builds it again. --soft, as make
# dev gives, goes on without a build when npm is missing.
#
#	sh tools/webshark/build.sh [-f | --soft]
set -eu

COMMIT=aa7f827f45d4e309f1ce8219b004691d21b4c029
REPO=https://github.com/hepictel/webshark

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out="$root/resources/webshark"

if [ "${1:-}" != "-f" ] && [ "$(cat "$out/COMMIT" 2>/dev/null)" = "$COMMIT" ]; then
	cp "$here/README.md" "$out/README.md"
	exit 0
fi
if ! command -v npm >/dev/null 2>&1; then
	if [ "${1:-}" = "--soft" ]; then
		echo "webshark: npm is not installed, so this build has no webshark: captures won't open in it"
		exit 0
	fi
	echo "webshark: npm is not installed; install Node.js to build webshark" >&2
	exit 1
fi
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

git clone -q "$REPO" "$work/webshark"
git -C "$work/webshark" checkout -q "$COMMIT"
npm --prefix "$work/webshark/ui" ci --no-audit --no-fund
npm --prefix "$work/webshark/ui" run build:kiosk

rm -rf "$out"
cp -R "$work/webshark/ui/dist/webshark" "$out"
find "$out" -type f -size +1M ! -name '*.gz' | while read -r f; do
	if [ -f "$f.gz" ]; then
		rm "$f"
	else
		gzip -9 -n "$f"
	fi
done
cp "$work/webshark/LICENSE" "$out/LICENSE"
cp "$here/README.md" "$out/README.md"
printf '%s\n' "$COMMIT" > "$out/COMMIT"
echo "webshark $COMMIT built into $out ($(du -sh "$out" | cut -f1))"
