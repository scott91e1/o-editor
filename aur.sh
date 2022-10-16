#!/bin/sh

VERSION=2.57.0

rm -rf archpackages
mkdir -p archpackages
cd archpackages

git clone ssh://aur@aur.archlinux.org/o-bin.git o-bin
git clone ssh://aur@aur.archlinux.org/o.git o
sed -r -i "s/2\.[[:digit:]]+\.[[:digit:]]+/$VERSION/g" o-bin/PKGBUILD o/PKGBUILD

#cd ../o-bin
#makepkg --verifysource > sums
