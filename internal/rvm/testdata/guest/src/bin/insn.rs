//! The instruction snippets of the rvm unit tests.
//!
//! The file insn.s holds each snippet of the Go tests once. The Go test
//! (build_test.go) writes insn.s, builds this program, and copies the bytes
//! of each snippet to ../../../insn.golden. So the encodings in the unit
//! tests come from the assembler of the Rust toolchain, not from the package
//! under test.
//!
//! Nothing runs this program as a whole.
#![no_std]
#![no_main]

core::arch::global_asm!(include_str!("insn.s"));

#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    loop {}
}
