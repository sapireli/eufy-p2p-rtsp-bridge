# Exact-kernel Broadcom module build and ABI gates

This records an isolated build, not a module load or playback result. The target
is Raspberry Pi ARMv6 kernel `6.18.50+rpt-rpi-v6`, package version
`1:6.18.50-1+rpt1`. The running module was read separately by the device agent;
no Pi package, kernel, module, network or boot change was made by this build.

## Matching source and header provenance

The [official Raspberry Pi trixie package index](https://archive.raspberrypi.com/debian/dists/trixie/main/binary-armhf/Packages.gz)
and [source index](https://archive.raspberrypi.com/debian/dists/trixie/main/source/Sources.gz)
provided exact-version filenames and hashes; downloaded packages were verified.
All three header/build packages below have version `1:6.18.50-1+rpt1`.

| Package | SHA-256 |
| --- | --- |
| linux-headers-6.18.50+rpt-common-rpi | `551be6bbd198c9fd7a4ae2fb9b1ecb4a613d47bc42769155dc8eac3f956afcf8` |
| linux-headers-6.18.50+rpt-rpi-v6 | `8979019086ede28b6e70d82e3841139426997ddcfede0ab3d6dcced070df719e` |
| linux-kbuild-6.18.50+rpt | `8bf21f15179b57777f73d4761524f8c8d430e336e1037b405da448e446ca31e5` |

Source archive `linux_6.18.50.orig.tar.xz`:
`d2fc041dab4e11d9645e3ba53be058faa52b8ce28a8a97c889bb2cacee170461`.
Packaging archive `linux_6.18.50-1+rpt1.debian.tar.xz`:
`98c2e147fda1eac197a221e6fb824e794c04830112d306f32c836433833e355e`.
The package's `debian/patches/rpi/rpi.patch` changes 16 Broadcom subtree files;
those exact changes were applied before the accepted stock build. Filtered patch
SHA-256: `d60f98c257be4c5393bf2e15b54c63f4ba0c616494baa9d768a083552c2fe0ec`.
The package patch does not change the audited reorder file. Its source remains
`e7de3c68cc4af9b4f26cb48dcd326403be77bf2fb1f2ba7d49ddc6c9791e32cd`.
An earlier orig-only build was rejected; it is not the accepted baseline.

Exact header `.config` SHA-256:
`4f6b35c591e715b91d43917ef9b282f53f1108dae3d9459fc7cf9e866ebd1407`.
Exact packaged `Module.symvers` (1,154,941 bytes) SHA-256:
`8c640aa38324bc44e1c60082a53ddcd468fdbcf69efea847f8c2b42abe0c8d2e`.
Installed stock core's 256 imported symbol CRCs and WCC's eight imports all match
that packaged symbol file before any rebuild.

## Isolated toolchain and actual build adjustments

The build ran on the Debian x86 host with `nice -n 19`, `make -j1` and extracted
packages in `/opt/ewb-brcmfmac-build`. No compiler/package was installed systemwide.
Native-host crosscompiler is GCC 14.2.0-19; the target package records Raspbian
14.2.0-19+rpi1. This is a toolchain packaging difference; byte-identical binary
output is not claimed. Tool package hashes:

| Tool package/version | SHA-256 |
| --- | --- |
| binutils-arm-linux-gnueabihf 2.44-3 | `9733b7a7a320da0ca637d71e69ab663c64f550ad542a848e5755c8170c8caf09` |
| cpp-14-arm-linux-gnueabihf 14.2.0-19cross1 | `e503180c7e7bb8451b33acd124872c7a43f45903913e8c96467e5ed386f7e434` |
| gcc-14-arm-linux-gnueabihf-base 14.2.0-19cross1 | `b7c87242d5ed79e0a789cf1ed5b790e69adce6eebf94291c34540c13c9af6107` |
| gcc-14-arm-linux-gnueabihf 14.2.0-19cross1 | `2caeee6eee62b44503939ba93fc75ade0f018e839cfc3207a5e2350864c2d147` |
| libc6-armhf-cross 2.41-11cross1 | `fa9bcf96a12d15a2e47396c31b3203e107a04baf25e324ecf008931c09320074` |
| libgcc-14-dev-armhf-cross 14.2.0-19cross1 | `f55933295979b017f7e94755396b95cf9fd59042f8c630ced39183dbe6711eaf` |
| qemu-user 1:10.0.13+ds-0+deb13u1 | `ca6ede739327a20ae5a3498230c8a3a9a072c586e131bc6bcc1201eb1725e336` |

`ARCH=arm`, explicit extracted cross `CC`, `NM`, `CPP`, `PATH` and
`LD_LIBRARY_PATH` were supplied. `KCFLAGS=-marm` explicitly matches target ARM mode
because the Debian crosscompiler defaults to Thumb. The parent Broadcom Makefile
was used, preserving its configured `-DDEBUG` and normal source flags.
The exact packaged ARM kbuild helpers execute under qemu-arm with extracted libc;
modpost directly uses its packaged `modpost.real-lsb-32` variant because its
multi-target launcher cannot chain ARM executables without system binfmt.
No version checks were disabled or symbol CRCs copied into generated objects.

An early tool PATH error silently prevented two brcmutil objects generating
symbol versions; the resulting 241-import core was rejected. Those objects were
rebuilt with explicit tools. The accepted final stock/candidate builds emit no
missing-CRC warning. This failure and dispatcher adjustment are retained in the
build record rather than presented as an immediately successful crossbuild.

## Accepted stock and candidate gates

| Artifact | Imported symbol CRCs | srcversion | SHA-256 |
| --- | --- | --- | --- |
| Rebuilt stock core | 256/256 exact installed stock | `94DA3F3023073BA79C36394` (exact installed) | `a6fc3d2b74b10e73bda9a34c18bf2c5be606fc583c517a9871c6f21f442010e0` |
| Candidate core | 256/256 exact installed stock | `5BB8E69CE3AF7690730352F` | `8b1545f74df0fe2292c098d8b62bef1921b168091dd4a8b25199715a5545c4af` |
| Rebuilt WCC (unchanged candidate) | 8/8 exact installed stock | `716D73DB84C375567734E4D` (exact installed) | `b91333438c4739a4dc19073e092060412af0d22b873f1e30f0846ebbd165db03` |

All vermagic strings exactly match installed stock:
`6.18.50+rpt-rpi-v6 mod_unload modversions ARMv6 p2v8`.
Candidate exported `Module.symvers` is byte-identical to the accepted stock build.
Candidate patch SHA-256:
`944a5ad5b080f283999e20b564402d3c372991fc6eb2d05defb92a71ea1a1b04`.
Candidate reorder source SHA-256:
`d66ff859f69af6f14f2dc7dbc9c0a95f400423993ee7d57c7e31212c971593e0`.
The stock artifacts were preserved before the incremental candidate build.
The original installed compressed stock module/boot path remains the rollback
reference; a rebuilt stock module is a separate matched-source control.

## Artifacts and limits

Local staging: `/tmp/ewb-brcmfmac-build/artifacts/{stock-built,candidate-built}`.
Private preserved record:
`.evidence/frame-freshness-2026-10-12/kernel-build/cross-build/`, including modules,
commands, final build logs, package/source manifests, ABI JSON and artifact hashes.
The source archives remain in local staging; their recorded hashes identify them.
All build jobs completed. No candidate was installed or loaded by this work.

These are build, source and ABI gates. They do not prove packet-order semantics
for every firmware sequence, compatibility under concurrency, elimination of live
warnings, or improved playback. A guarded fresh-stock/candidate comparison must
separately distinguish an accounting repair from simply resetting an already
corrupt ring, preserve original module files and provide network-independent
rollback before any live claim.
