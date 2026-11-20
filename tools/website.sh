#!/bin/sh
# website.sh builds and publishes gorex.goclaw.org: the Makefile's
# website target runs "website.sh build", website-sync "website.sh sync",
# github-release "website.sh github",
# with the settings below as NAME=value arguments. The Makefile runs it
# in a subshell of its recipe's shell ("(set -- build ...; . website.sh)"),
# as on Windows a shell started from there gets no environment.
#
#   NAME, VERSION     the app's, from mygo.json
#   SITE_URL          https://gorex.goclaw.org
#   SITE_PLATFORMS    GOOS/GOARCH, comma separated: darwin and linux ones
#                     build on MAC_HOST, the others here
#   SITE_BUILD        where the builds go, relative to the checkout
#   UPDATE_KEY        the secret key of mygo keygen, which signs updates
#   MAC_HOST, MAC_DIR the Mac that builds darwin and linux, and its checkout, whose
#                     update key is in its mygo configuration directory
#   SSH               ssh, for the Mac
#   SITE_HOST, SITE_DIR, SITE_OWNER, RSYNC, RSYNC_SSH   where sync uploads
#   GITHUB_REPO, GH   the GitHub repository github releases to, and gh
#
# build builds the platforms whose published version is older than
# VERSION, refusing one whose published version is newer, and stages the
# site in website/: updates/ (the signed archives, deltas, manifests,
# installers, Linux's install.sh) and download/ (each platform's installer
# under a name that stays). sync uploads it: updates/ and download/ only
# add and replace, the manifests last, so that apps never see a manifest
# whose archive is missing; the rest is mirrored, but for ISPConfig's
# error/, stats/ and robots.txt. github makes the version a release on
# GitHub, with the installers only, once the site has it.
set -eu

die() {
	echo "website: $*" >&2
	exit 1
}

# newer succeeds when version $2 is newer than $1, number by number (awk:
# the sort on Windows' PATH may be its own, without -V).
newer() {
	awk -v a="$1" -v b="$2" 'BEGIN { split(a, x, "."); split(b, y, "."); for (i = 1; i <= 3; i++) { if (y[i] + 0 > x[i] + 0) exit 0; if (y[i] + 0 < x[i] + 0) exit 1 } exit 1 }'
}

build() {
	[ -f "$UPDATE_KEY" ] || die "no update key found at $UPDATE_KEY (set UPDATE_KEY)"
	# The Mac builds the commit from the fork, and a release is of a
	# commit anyway.
	git diff --quiet HEAD || die "the working tree has uncommitted changes. Commit them first."
	git fetch -q fork
	head=$(git rev-parse HEAD)
	[ "$head" = "$(git rev-parse fork/master)" ] || die "HEAD is not the same as fork/master. Push it first."

	here="" mac=""
	mkdir -p "$(dirname "$SITE_BUILD")"
	for p in $(echo "$SITE_PLATFORMS" | tr , ' '); do
		t=$(echo "$p" | tr / -)
		manifest="$SITE_URL/updates/update-$t.json"
		code=$(curl -sS -o "$SITE_BUILD.json" -w '%{http_code}' "$manifest") || die "could not read $manifest"
		if [ "$code" = 200 ]; then
			# The manifest's own version is the first: those after it are
			# of the releases before it.
			v=$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$SITE_BUILD.json" | head -n 1)
			if [ "$v" = "$VERSION" ]; then
				echo "website: $p: $VERSION is already published"
				continue
			fi
			newer "$v" "$VERSION" || die "$p: $v is already published, so $VERSION must be newer"
		elif [ "$code" != 404 ]; then
			die "$manifest: HTTP $code"
		fi
		# The Mac builds darwin, which only it can sign, and Linux, whose
		# archives mygo build makes without their files' modes on Windows,
		# so that gorex would not be executable.
		case $p in
		darwin/* | linux/*) mac="$mac,$p" ;;
		*) here="$here,$p" ;;
		esac
	done
	rm -f "$SITE_BUILD.json"
	here=${here#,} mac=${mac#,}
	[ -n "$here$mac" ] || die "$VERSION is already published for every platform. A release needs a new version."

	rm -rf "$SITE_BUILD"
	if [ -n "$here" ]; then
		echo "website: building $here"
		MYGO_UPDATER_PRIVATE_KEY=$(cat "$UPDATE_KEY") go tool mygo build -platform "$here" -o "$SITE_BUILD"
	fi
	if [ -n "$mac" ]; then
		echo "website: building $mac on $MAC_HOST"
		$SSH "$MAC_HOST" "set -e; cd $MAC_DIR; git fetch -q origin; git checkout -q -B master $head; rm -rf $SITE_BUILD;
			MYGO_UPDATER_PRIVATE_KEY=\"\$(cat \"\$HOME/Library/Application Support/mygo/update-keys/mygo-update.key\")\" go tool mygo build -platform $mac -o $SITE_BUILD"
		for p in $(echo "$mac" | tr , ' '); do
			t=$(echo "$p" | tr / -)
			mkdir -p "$SITE_BUILD/$t"
			$SSH "$MAC_HOST" "cd $MAC_DIR/$SITE_BUILD/$t && tar -cf - \$(ls *.dmg *.deb *.tar.gz update-*.json install.sh *.delta 2>/dev/null)" | tar -xf - -C "$SITE_BUILD/$t"
		done
	fi
	stage
}

# stage puts the builds in website/.
stage() {
	rm -rf website/download website/updates
	mkdir -p website/download website/updates
	cp resources/icon.png website/icon.png
	cp docs/screenshot.png website/screenshot.png
	# mygo build names its installers in lower case, as
	# gorex-setup-0.1.1-amd64.exe and gorex-0.1.1-arm64.dmg.
	lname=$(echo "$NAME" | tr '[:upper:]' '[:lower:]')
	for d in "$SITE_BUILD"/*/; do
		t=$(basename "$d")
		cp "$d"*.tar.gz "$d"update-*.json website/updates/
		for f in "$d"*.delta; do
			[ ! -e "$f" ] || cp "$f" website/updates/
		done
		case $t in
		windows-amd64)
			cp "$d$lname-setup-$VERSION-amd64.exe" website/updates/
			cp "$d$lname-setup-$VERSION-amd64.exe" "website/download/$NAME-Setup-amd64.exe"
			;;
		darwin-arm64)
			cp "$d$lname-$VERSION-arm64.dmg" website/updates/
			cp "$d$lname-$VERSION-arm64.dmg" "website/download/$NAME-arm64.dmg"
			;;
		linux-amd64)
			cp "${d}install.sh" website/updates/
			for f in "$d"*.deb; do
				cp "$f" website/updates/
				cp "$f" website/download/gorex-amd64.deb
			done
			;;
		esac
	done
	ls -l website/download website/updates
}

sync() {
	[ -n "$SITE_OWNER" ] || die "set SITE_OWNER to the user:group of the ISPConfig site, for example web7:client1"
	ls website/updates/update-*.json >/dev/null 2>&1 || die "nothing is built yet. Run make website first."
	up() {
		"$RSYNC" -rtpz --chown="$SITE_OWNER" --chmod=D755,F644 -e "$RSYNC_SSH" "$@"
	}
	up --exclude='update-*.json' website/updates/ "$SITE_HOST:${SITE_DIR}updates/"
	up website/download/ "$SITE_HOST:${SITE_DIR}download/"
	up website/updates/update-*.json "$SITE_HOST:${SITE_DIR}updates/"
	up --delete --filter='P /updates/' --filter='P /download/' --filter='P /error/' --filter='P /stats/' --filter='P /robots.txt' \
		--exclude=/updates/ --exclude=/download/ website/ "$SITE_HOST:$SITE_DIR"
	echo "website: published to $SITE_URL"
}

# github makes VERSION a release on GitHub (GITHUB_REPO), once the site
# has it on every platform: tag vVERSION on the commit that set mygo.json
# to VERSION (the release commit, which the fork must have), pushed to the
# fork; the version's section of CHANGELOG.md as its notes; and as its
# files the installers that website/updates has from make website, Linux's
# install.sh, and their SHA256SUMS. Run again, it uploads them again.
github() {
	git fetch -q fork
	rel=$(git log -1 --format=%H -S"\"version\": \"$VERSION\"" -- mygo.json)
	[ -n "$rel" ] || die "no commit sets mygo.json's version to $VERSION"
	git merge-base --is-ancestor "$rel" fork/master || die "the fork does not have $rel, the release commit of $VERSION. Push it first."
	out="$SITE_BUILD-github"
	rm -rf "$out"
	mkdir -p "$out"
	lname=$(echo "$NAME" | tr '[:upper:]' '[:lower:]')
	for p in $(echo "$SITE_PLATFORMS" | tr , ' '); do
		t=$(echo "$p" | tr / -)
		v=$(curl -fsS "$SITE_URL/updates/update-$t.json" | sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' | head -n 1) || die "could not read the site's manifest for $p"
		[ "$v" = "$VERSION" ] || die "$p: the site has ${v:-no version}, not $VERSION. Publish it first (make publish)."
		case $t in
		windows-amd64) files="$lname-setup-$VERSION-amd64.exe" ;;
		darwin-arm64) files="$lname-$VERSION-arm64.dmg" ;;
		linux-amd64) files="${lname}_${VERSION}_amd64.deb install.sh" ;;
		*) files="" ;;
		esac
		for f in $files; do
			[ -f "website/updates/$f" ] || die "website/updates/$f is missing. It comes from make website."
			cp "website/updates/$f" "$out/"
		done
	done
	(cd "$out" && sha256sum -- * >SHA256SUMS)
	awk -v h="## $VERSION" '{ sub(/\r$/, "") } $0 == h { f = 1; next } f && /^## / { exit } f' CHANGELOG.md >"$out.md"
	grep -q . "$out.md" || die "CHANGELOG.md has no section for $VERSION"

	tag="v$VERSION"
	if t=$(git rev-parse -q --verify "refs/tags/$tag^{commit}"); then
		[ "$t" = "$rel" ] || die "tag $tag is on $t, not on $rel, the release commit of $VERSION"
	else
		# A plain tag: an annotated one needs the user's identity, which
		# git can't find from the Makefile's shell on Windows.
		git tag "$tag" "$rel"
	fi
	git push -q fork "refs/tags/$tag"
	if "$GH" release view "$tag" -R "$GITHUB_REPO" >/dev/null 2>&1; then
		"$GH" release edit "$tag" -R "$GITHUB_REPO" --title "$NAME $VERSION" --notes-file "$out.md"
		"$GH" release upload "$tag" -R "$GITHUB_REPO" --clobber "$out"/*
	else
		"$GH" release create "$tag" -R "$GITHUB_REPO" --verify-tag --title "$NAME $VERSION" --notes-file "$out.md" "$out"/*
	fi
	echo "github: released $tag at https://github.com/$GITHUB_REPO/releases/tag/$tag"
}

cmd=${1:-}
[ $# -gt 0 ] && shift
for a in "$@"; do
	case $a in
	[A-Z]*=*) export "$a" ;;
	*) die "not a setting (expected NAME=value): $a" ;;
	esac
done
case $cmd in
build) build ;;
sync) sync ;;
github) github ;;
*) die "usage: website.sh build|sync|github [NAME=value]..." ;;
esac
