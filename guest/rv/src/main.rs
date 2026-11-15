//! The kernel program for the machine of `internal/rvm`: RV32IMAC, bare
//! metal, no runtime. `docs/vm.md` defines the machine.
//!
//! The machine gives the program a stack pointer and three environment
//! calls: halt, read, and write. This file adds what the kernel needs and the
//! machine does not give: a heap.
#![no_std]
#![no_main]

use clearway_kernel::{run, Input, Output};
use core::alloc::{GlobalAlloc, Layout};
use core::arch::asm;
use core::panic::PanicInfo;
use core::ptr::addr_of_mut;

// The environment calls of the machine.
const HALT: u32 = 0;
const READ: u32 = 1;
const WRITE: u32 = 2;

// Exit codes of this file. 0 is success, and 2 to 4 are `clearway_kernel::Error`.
const EXIT_PANIC: u32 = 101;

fn ecall(number: u32, a0: u32, a1: u32) -> u32 {
    let result: u32;
    // SAFETY: an environment call of the machine reads a0, a1, a7 and writes
    // a0. It touches no other register and only the memory that a0 and a1
    // describe, which the callers below own.
    unsafe {
        asm!("ecall", inlateout("a0") a0 => result, in("a1") a1, in("a7") number);
    }
    result
}

fn halt(code: u32) -> ! {
    ecall(HALT, code, 0);
    loop {}
}

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    // A panic is a defect in the kernel: an overflow, an index out of range,
    // or a heap that is full. The machine reports the exit code.
    halt(EXIT_PANIC)
}

const BUFFER: usize = 4096;

struct In {
    buf: [u8; BUFFER],
    pos: usize,
    len: usize,
}

impl Input for In {
    fn read(&mut self, out: &mut [u8]) -> bool {
        for b in out {
            if self.pos == self.len {
                // The machine fills the buffer, or gives what is left of the
                // input. Zero bytes is the end.
                self.len = ecall(READ, self.buf.as_mut_ptr() as u32, BUFFER as u32) as usize;
                self.pos = 0;
                if self.len == 0 {
                    return false;
                }
            }
            *b = self.buf[self.pos];
            self.pos += 1;
        }
        true
    }
}

struct Out {
    buf: [u8; BUFFER],
    len: usize,
}

impl Out {
    fn flush(&mut self) {
        if self.len > 0 {
            ecall(WRITE, self.buf.as_ptr() as u32, self.len as u32);
            self.len = 0;
        }
    }
}

impl Output for Out {
    fn write(&mut self, data: &[u8]) {
        for &b in data {
            if self.len == BUFFER {
                self.flush();
            }
            self.buf[self.len] = b;
            self.len += 1;
        }
    }
}

/// The entry point. The machine has set the stack pointer.
#[no_mangle]
pub extern "C" fn _start() -> ! {
    let mut input = In { buf: [0; BUFFER], pos: 0, len: 0 };
    let mut out = Out { buf: [0; BUFFER], len: 0 };
    let code = match run(&mut input, &mut out) {
        Ok(()) => 0,
        Err(e) => e as u32,
    };
    out.flush();
    halt(code)
}

// ------------------------------------------------------------------- heap
//
// One block of memory, cut into blocks whose sizes are powers of two. A free
// block goes on the list of its size and is used again for the next request
// of that size. The program has one thread and no interrupts.

const HEAP_SIZE: usize = 40 << 20;
const MIN_BLOCK: usize = 16;
/// A block is aligned to its size, up to this.
const MAX_ALIGN: usize = 4096;

static mut HEAP: [u8; HEAP_SIZE] = [0; HEAP_SIZE];
static mut NEXT: usize = 0;
static mut FREE: [usize; 32] = [0; 32];

struct Heap;

fn block(layout: Layout) -> (usize, usize) {
    let size = layout.size().max(layout.align()).max(MIN_BLOCK).next_power_of_two();
    (size, size.trailing_zeros() as usize)
}

unsafe impl GlobalAlloc for Heap {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        if layout.align() > MAX_ALIGN {
            return core::ptr::null_mut();
        }
        let (size, class) = block(layout);
        // SAFETY: one thread, no interrupts: nothing else reads or writes
        // these statics. A block on a free list holds the address of the next
        // one in its first word, and every block is at least 16 bytes.
        let free = addr_of_mut!(FREE);
        let head = (*free)[class];
        if head != 0 {
            (*free)[class] = *(head as *const usize);
            return head as *mut u8;
        }
        let align = size.min(MAX_ALIGN);
        let base = addr_of_mut!(HEAP) as usize;
        let next = addr_of_mut!(NEXT);
        let start = (base + *next + align - 1) & !(align - 1);
        let end = match start.checked_add(size) {
            Some(end) if end <= base + HEAP_SIZE => end,
            _ => return core::ptr::null_mut(),
        };
        *next = end - base;
        start as *mut u8
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        let (_, class) = block(layout);
        // SAFETY: as in alloc. ptr came from alloc with the same layout, so
        // it is a block of this class.
        let free = addr_of_mut!(FREE);
        *(ptr as *mut usize) = (*free)[class];
        (*free)[class] = ptr as usize;
    }
}

#[global_allocator]
static ALLOCATOR: Heap = Heap;
