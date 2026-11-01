#!/bin/sh
# Builds the libacars the tests compare against, at the commit the port follows, into third_party/install.
# Needs git, cmake, pkg-config and the development files of jansson, zlib and libxml2
# (Debian: sudo apt install build-essential git cmake pkg-config libjansson-dev zlib1g-dev libxml2-dev;
#  macOS: brew install cmake pkg-config jansson libxml2 zlib).
set -eu
commit=9af09a0121d4ec577339cbd4c7420d7519da48fa   # libacars 2.2.1
root=$(cd "$(dirname "$0")/.." && pwd)
src=$root/third_party/libacars
if [ ! -d "$src/.git" ]; then
	mkdir -p "$root/third_party"
	git init -q "$src"
	git -C "$src" remote add origin https://github.com/szpajder/libacars.git
fi
git -C "$src" fetch -q --depth 1 origin "$commit"
git -C "$src" checkout -q --detach "$commit"
rm -rf "$src/build"
mkdir "$src/build"
cd "$src/build"
cmake .. \
	-DCMAKE_INSTALL_PREFIX="$root/third_party/install" \
	-DCMAKE_INSTALL_RPATH="$root/third_party/install/lib" \
	-DCMAKE_POLICY_VERSION_MINIMUM=3.5
make -j4
make install
# The comparison is only meaningful if libacars has the optional parts on.
for opt in WITH_ZLIB WITH_JANSSON; do
	grep -q "^#define $opt" libacars/config.h || { echo "libacars was built without $opt; install its development package and run again" >&2; exit 1; }
done
echo "libacars built in $root/third_party/install"
