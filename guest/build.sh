#!/bin/sh
# Builds the kernel program for the machine of internal/rvm and puts it where
# the Go code embeds it: internal/kimage/kernel.elf. Then it translates the
# program into host code, internal/kimage/kcode.go, which the machine runs in
# place of interpreting (docs/vm.md).
#
# The program is the published form of the kernel: its SHA-256 names it. Run
# this after a change to guest/kernel or guest/rv, and commit the two new
# files with the new digest in internal/kimage/kimage.go.
#
# It needs cargo, with the target riscv32imac-unknown-none-elf, Go, and no
# network.
set -eu
cd "$(dirname "$0")"
# Paths of this checkout must not reach the program.
RUSTFLAGS="--remap-path-prefix=$(pwd)=guest --remap-path-prefix=${CARGO_HOME:-$HOME/.cargo}=cargo --remap-path-prefix=${RUSTUP_HOME:-$HOME/.rustup}=rustup" \
  cargo build --release --offline -p clearway-kernel-rv --target riscv32imac-unknown-none-elf
cp target/riscv32imac-unknown-none-elf/release/kernel ../internal/kimage/kernel.elf
(cd .. && go run ./cmd/rvgen -pkg kimage -name kernelCode -o internal/kimage/kcode.go internal/kimage/kernel.elf)
shasum -a 256 ../internal/kimage/kernel.elf
