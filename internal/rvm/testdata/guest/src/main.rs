//! The guest program of the rvm tests.
//!
//! The program reads requests from the input stream until the input ends. It
//! writes one response for each request. The Go test (guest_test.go) makes
//! the requests, computes each response natively, and compares the bytes.
//!
//! A request is one byte that names the operation, the length of the payload
//! as a little-endian u32, and the payload. A response is the length of its
//! payload as a little-endian u32, and the payload. Every number in a payload
//! is little-endian.
//!
//! The program needs from the machine only what the machine gives at the
//! start: a stack pointer, and memory that is zero outside the loaded
//! segments.
#![no_std]
#![no_main]

mod sha256;

use core::arch::asm;
use core::cell::UnsafeCell;
use core::cmp::Ordering as Cmp;
use core::panic::PanicInfo;
use core::ptr;
use core::sync::atomic::{AtomicI32, AtomicU32, Ordering};

// The environment calls of the machine.
const SYS_HALT: u32 = 0;
const SYS_READ: u32 = 1;
const SYS_WRITE: u32 = 2;

// The operations.
const OP_ARITH64: u8 = 1;
const OP_ARITH128: u8 = 2;
const OP_SHA256: u8 = 3;
const OP_MEM: u8 = 4;
const OP_RECURSE: u8 = 5;
const OP_FNTABLE: u8 = 6;
const OP_STATICS: u8 = 7;
const OP_HEAP: u8 = 8;
const OP_ATOMIC: u8 = 9;
const OP_EXIT: u8 = 10;
const OP_RAW_M: u8 = 11;

// The exit codes of a failure. The end of the input gives the exit code 0.
const EXIT_SHORT_HEADER: u32 = 0xE001;
const EXIT_LONG_PAYLOAD: u32 = 0xE002;
const EXIT_SHORT_PAYLOAD: u32 = 0xE003;
const EXIT_UNKNOWN_OP: u32 = 0xE004;
const EXIT_LONG_RESPONSE: u32 = 0xE005;
const EXIT_BAD_PAYLOAD: u32 = 0xE006;
const EXIT_PANIC: u32 = 0xE0FF;

const REQ_MAX: usize = 1 << 16;
const RESP_MAX: usize = 1 << 18;
const HEAP_SIZE: usize = 1 << 20;
const SCRATCH_SIZE: usize = 4096;

/// Makes one environment call and returns the value of a0 after it.
#[inline(always)]
fn ecall(number: u32, a0: u32, a1: u32) -> u32 {
    let result: u32;
    unsafe {
        asm!(
            "ecall",
            in("a7") number,
            inlateout("a0") a0 => result,
            in("a1") a1,
            options(nostack),
        );
    }
    result
}

fn halt(code: u32) -> ! {
    ecall(SYS_HALT, code, 0);
    loop {}
}

/// Reads from the input until `buf` is full or the input ends. Returns the
/// count of bytes read.
fn read_full(buf: &mut [u8]) -> usize {
    let mut done = 0;
    while done < buf.len() {
        let rest = &mut buf[done..];
        let n = ecall(SYS_READ, rest.as_mut_ptr() as u32, rest.len() as u32) as usize;
        if n == 0 {
            break;
        }
        done += n;
    }
    done
}

fn write_all(buf: &[u8]) {
    ecall(SYS_WRITE, buf.as_ptr() as u32, buf.len() as u32);
}

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    halt(EXIT_PANIC)
}

/// A static that the single thread of the guest changes.
struct Global<T>(UnsafeCell<T>);

unsafe impl<T> Sync for Global<T> {}

impl<T> Global<T> {
    const fn new(value: T) -> Self {
        Global(UnsafeCell::new(value))
    }

    #[allow(clippy::mut_from_ref)]
    fn get(&self) -> &mut T {
        // The guest has one thread, and no caller keeps two references to
        // the same static.
        unsafe { &mut *self.0.get() }
    }
}

/// Gives its content an address that is a multiple of 16.
#[repr(align(16))]
struct Aligned<T>(T);

// Zeroed statics; the linker puts them in .bss.
static REQ: Global<[u8; REQ_MAX]> = Global::new([0; REQ_MAX]);
static RESP: Global<[u8; RESP_MAX]> = Global::new([0; RESP_MAX]);
static HEAP: Global<Aligned<[u8; HEAP_SIZE]>> = Global::new(Aligned([0; HEAP_SIZE]));
static HEAP_USED: Global<usize> = Global::new(0);
static SCRATCH: Global<[u8; SCRATCH_SIZE]> = Global::new([0; SCRATCH_SIZE]);
static BSS_WORDS: Global<[u32; 8]> = Global::new([0; 8]);

// Statics with a start value; the linker puts them in .data.
static DATA_WORDS: Global<[u32; 8]> = Global::new([
    0x0123_4567,
    0x89ab_cdef,
    0xdead_beef,
    0x0bad_f00d,
    0x1357_9bdf,
    0x2468_ace0,
    0xffff_ffff,
    0x0000_0001,
]);
static DATA_BYTES: Global<[u8; 16]> = Global::new(*b"clearway guest!\n");
static ATOM_U: AtomicU32 = AtomicU32::new(0x8000_0000);
static ATOM_I: AtomicI32 = AtomicI32::new(-7);

// A constant table; the linker puts it in .rodata.
static RO_WORDS: [u32; 8] = [
    0x9e37_79b9,
    0x7f4a_7c15,
    0xf39c_c060,
    0x5ced_c834,
    0x1082_276b,
    0xf3a2_7251,
    0xf86c_6a11,
    0xd0c1_8e95,
];

/// Reads the numbers of a request payload.
struct Reader<'a> {
    buf: &'a [u8],
}

impl<'a> Reader<'a> {
    fn left(&self) -> usize {
        self.buf.len()
    }

    fn bytes(&mut self, n: usize) -> &'a [u8] {
        if self.buf.len() < n {
            halt(EXIT_BAD_PAYLOAD);
        }
        let (head, tail) = self.buf.split_at(n);
        self.buf = tail;
        head
    }

    fn u8(&mut self) -> u8 {
        self.bytes(1)[0]
    }

    fn u16(&mut self) -> u16 {
        let mut b = [0; 2];
        b.copy_from_slice(self.bytes(2));
        u16::from_le_bytes(b)
    }

    fn u32(&mut self) -> u32 {
        let mut b = [0; 4];
        b.copy_from_slice(self.bytes(4));
        u32::from_le_bytes(b)
    }

    fn u64(&mut self) -> u64 {
        let mut b = [0; 8];
        b.copy_from_slice(self.bytes(8));
        u64::from_le_bytes(b)
    }

    fn u128(&mut self) -> u128 {
        let mut b = [0; 16];
        b.copy_from_slice(self.bytes(16));
        u128::from_le_bytes(b)
    }
}

/// Collects one response in RESP.
struct Writer {
    len: usize,
}

impl Writer {
    fn new() -> Self {
        // The first four bytes hold the length of the payload.
        Writer { len: 4 }
    }

    fn bytes(&mut self, b: &[u8]) {
        let end = self.len + b.len();
        if end > RESP_MAX {
            halt(EXIT_LONG_RESPONSE);
        }
        RESP.get()[self.len..end].copy_from_slice(b);
        self.len = end;
    }

    fn u8(&mut self, v: u8) {
        self.bytes(&[v]);
    }

    fn u32(&mut self, v: u32) {
        self.bytes(&v.to_le_bytes());
    }

    fn u64(&mut self, v: u64) {
        self.bytes(&v.to_le_bytes());
    }

    fn u128(&mut self, v: u128) {
        self.bytes(&v.to_le_bytes());
    }

    fn flush(self) {
        let resp = RESP.get();
        resp[..4].copy_from_slice(&((self.len - 4) as u32).to_le_bytes());
        write_all(&resp[..self.len]);
    }
}

/// OP_ARITH64. Payload: records of a (u64), b (u64), s (u8).
fn arith64(r: &mut Reader, w: &mut Writer) {
    while r.left() >= 17 {
        let (a, b, s) = (r.u64(), r.u64(), r.u8());
        let (sa, sb) = (a as i64, b as i64);
        let sh = (s & 63) as u32;

        w.u64(a.wrapping_add(b));
        w.u64(a.wrapping_sub(b));
        w.u64(a.wrapping_mul(b));
        w.u64(if b != 0 { a / b } else { u64::MAX });
        w.u64(if b != 0 { a % b } else { a });
        w.u64(if b != 0 { sa.wrapping_div(sb) as u64 } else { u64::MAX });
        w.u64(if b != 0 { sa.wrapping_rem(sb) as u64 } else { a });
        w.u64(a << sh);
        w.u64(a >> sh);
        w.u64((sa >> sh) as u64);
        w.u64(a.rotate_left(sh));
        w.u32(a.leading_zeros());
        w.u32(a.trailing_zeros());
        w.u32(a.count_ones());
        w.u64(((a as u128 * b as u128) >> 64) as u64);
        w.u64(((sa as i128 * sb as i128) >> 64) as u64);
        w.u64(sa.saturating_mul(sb) as u64);
        w.u8((a < b) as u8
            | ((sa < sb) as u8) << 1
            | (a.checked_add(b).is_none() as u8) << 2
            | (sa.checked_sub(sb).is_none() as u8) << 3
            | (sa.checked_mul(sb).is_none() as u8) << 4);

        // The same on the low halves.
        let (x, y) = (a as u32, b as u32);
        let (sx, sy) = (x as i32, y as i32);
        w.u32(x.wrapping_mul(y));
        w.u32(if y != 0 { x / y } else { u32::MAX });
        w.u32(if y != 0 { x % y } else { x });
        w.u32(if y != 0 { sx.wrapping_div(sy) as u32 } else { u32::MAX });
        w.u32(if y != 0 { sx.wrapping_rem(sy) as u32 } else { x });
        w.u32(((x as u64 * y as u64) >> 32) as u32);
        w.u32(((sx as i64 * sy as i64) >> 32) as u32);
        w.u32(x.leading_zeros());
    }
}

/// Returns the integer square root of `n`, one bit at a time.
#[inline(never)]
fn isqrt_bits(n: u128) -> u64 {
    let mut rest = n;
    let mut root: u128 = 0;
    let mut bit: u128 = 1 << 126;
    while bit > rest {
        bit >>= 2;
    }
    while bit != 0 {
        if rest >= root + bit {
            rest -= root + bit;
            root = (root >> 1) + bit;
        } else {
            root >>= 1;
        }
        bit >>= 2;
    }
    root as u64
}

/// Returns the integer square root of `n`, by Newton's method.
#[inline(never)]
fn isqrt_newton(n: u128) -> u64 {
    if n < 2 {
        return n as u64;
    }
    // The start value is a power of two that is not less than the root.
    let mut x: u128 = 1 << ((128 - n.leading_zeros()).div_ceil(2));
    loop {
        let y = (x + n / x) >> 1;
        if y >= x {
            return x as u64;
        }
        x = y;
    }
}

/// OP_ARITH128. Payload: records of a (u128), b (u128), s (u8).
fn arith128(r: &mut Reader, w: &mut Writer) {
    while r.left() >= 33 {
        let (a, b, s) = (r.u128(), r.u128(), r.u8());
        let (sa, sb) = (a as i128, b as i128);
        let sh = (s & 127) as u32;

        w.u128(a.wrapping_add(b));
        w.u128(a.wrapping_sub(b));
        w.u128(a.wrapping_mul(b));
        w.u128(if b != 0 { a / b } else { u128::MAX });
        w.u128(if b != 0 { a % b } else { a });
        w.u128(if b != 0 { sa.wrapping_div(sb) as u128 } else { u128::MAX });
        w.u128(if b != 0 { sa.wrapping_rem(sb) as u128 } else { a });
        w.u128(a << sh);
        w.u128(a >> sh);
        w.u128((sa >> sh) as u128);
        w.u128(a.rotate_left(sh));
        w.u32(a.leading_zeros());
        w.u32(a.trailing_zeros());
        w.u32(a.count_ones());
        w.u64(isqrt_bits(a));
        w.u64(isqrt_newton(a));
        w.u128(a as u64 as u128 * b as u64 as u128);
        w.u128(((a as i64 as i128 * b as i64 as i128) >> 32) as u128);
        w.u8((a < b) as u8
            | ((sa < sb) as u8) << 1
            | (a.checked_add(b).is_none() as u8) << 2
            | (sa.checked_mul(sb).is_none() as u8) << 3);
    }
}

/// OP_MEM. Payload: seed (u32), then records of kind (u8), value (u8),
/// dst (u16), src (u16), len (u16).
fn mem(r: &mut Reader, w: &mut Writer) {
    let scratch = SCRATCH.get();
    let mut x = r.u32() | 1;
    for byte in scratch.iter_mut() {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        *byte = (x >> 24) as u8;
    }
    while r.left() >= 8 {
        let (kind, value) = (r.u8(), r.u8());
        let (dst, src, len) = (r.u16() as usize, r.u16() as usize, r.u16() as usize);
        if dst + len > SCRATCH_SIZE || src + len > SCRATCH_SIZE {
            halt(EXIT_BAD_PAYLOAD);
        }
        let p = scratch.as_mut_ptr();
        match kind {
            0 => unsafe { ptr::write_bytes(p.add(dst), value, len) },
            // The test gives ranges that do not overlap.
            1 => unsafe { ptr::copy_nonoverlapping(p.add(src), p.add(dst), len) },
            2 => unsafe { ptr::copy(p.add(src), p.add(dst), len) },
            3 => w.u8(match scratch[dst..dst + len].cmp(&scratch[src..src + len]) {
                Cmp::Less => 0xff,
                Cmp::Equal => 0,
                Cmp::Greater => 1,
            }),
            4 => w.u8((scratch[dst..dst + len] == scratch[src..src + len]) as u8),
            _ => halt(EXIT_BAD_PAYLOAD),
        }
    }
    w.bytes(scratch);
}

/// Folds the u32 values of `p` from the last to the first. The depth of the
/// recursion is the count of values.
#[inline(never)]
fn fold(p: &[u8]) -> u64 {
    if p.len() < 4 {
        return 0x9e37_79b9_7f4a_7c15;
    }
    let v = u32::from_le_bytes([p[0], p[1], p[2], p[3]]);
    let rest = fold(&p[4..]);
    (rest.rotate_left(5) ^ v as u64).wrapping_mul(0x0000_0100_0000_01b3)
}

/// Sorts `v` with a recursive quicksort.
#[inline(never)]
fn quicksort(v: &mut [u32]) {
    if v.len() < 2 {
        return;
    }
    // The pivot is not the last value, so each part is shorter than `v`.
    let pivot = v[(v.len() - 1) / 2];
    let (mut i, mut j) = (0, v.len() - 1);
    loop {
        while v[i] < pivot {
            i += 1;
        }
        while v[j] > pivot {
            j -= 1;
        }
        if i >= j {
            break;
        }
        v.swap(i, j);
        i += 1;
        j -= 1;
    }
    let (low, high) = v.split_at_mut(j + 1);
    quicksort(low);
    quicksort(high);
}

/// Takes `size` bytes at a multiple of `align` from the heap. Returns the
/// offset of the block, or None if the heap is full.
fn heap_alloc(size: usize, align: usize) -> Option<usize> {
    let used = HEAP_USED.get();
    let offset = (*used + align - 1) & !(align - 1);
    if offset > HEAP_SIZE || size > HEAP_SIZE - offset {
        return None;
    }
    *used = offset + size;
    Some(offset)
}

/// OP_RECURSE. Payload: u32 values.
fn recurse(r: &mut Reader, w: &mut Writer) {
    let count = r.left() / 4;
    let payload = r.bytes(4 * count);
    w.u64(fold(payload));

    // Sort a copy on the heap, then give the block back as zeros.
    let mark = *HEAP_USED.get();
    let Some(offset) = heap_alloc(4 * count, 4) else {
        halt(EXIT_BAD_PAYLOAD);
    };
    let block = &mut HEAP.get().0[offset..offset + 4 * count];
    block.copy_from_slice(payload);
    let values = unsafe { core::slice::from_raw_parts_mut(block.as_mut_ptr() as *mut u32, count) };
    quicksort(values);
    for v in values.iter() {
        w.u32(*v);
    }
    block.fill(0);
    *HEAP_USED.get() = mark;
}

#[inline(never)]
fn f_add(a: u32, b: u32) -> u32 {
    a.wrapping_add(b)
}

#[inline(never)]
fn f_sub(a: u32, b: u32) -> u32 {
    a.wrapping_sub(b)
}

#[inline(never)]
fn f_mul(a: u32, b: u32) -> u32 {
    a.wrapping_mul(b | 1)
}

#[inline(never)]
fn f_xrot(a: u32, b: u32) -> u32 {
    (a ^ b).rotate_left(b & 31)
}

#[inline(never)]
fn f_div(a: u32, b: u32) -> u32 {
    if b == 0 {
        !a
    } else {
        a / b
    }
}

#[inline(never)]
fn f_rem(a: u32, b: u32) -> u32 {
    if b == 0 {
        a
    } else {
        (a as i32).wrapping_rem(b as i32) as u32
    }
}

#[inline(never)]
fn f_mix(a: u32, b: u32) -> u32 {
    a.wrapping_mul(0x9e37_79b1) ^ b.swap_bytes()
}

#[inline(never)]
fn f_sar(a: u32, b: u32) -> u32 {
    ((a as i32) >> (b & 31)) as u32 ^ b
}

static FUNCS: [fn(u32, u32) -> u32; 8] = [f_add, f_sub, f_mul, f_xrot, f_div, f_rem, f_mix, f_sar];

/// OP_FNTABLE. Payload: records of index (u8), operand (u32).
fn fntable(r: &mut Reader, w: &mut Writer) {
    let mut acc: u32 = 0x1234_5678;
    while r.left() >= 5 {
        let (index, operand) = (r.u8(), r.u32());
        acc = FUNCS[(index & 7) as usize](acc, operand);
        w.u32(acc);
    }
}

/// OP_STATICS. Payload: records of kind (u8), value (u32).
fn statics(r: &mut Reader, w: &mut Writer) {
    let data = DATA_WORDS.get();
    let bytes = DATA_BYTES.get();
    let bss = BSS_WORDS.get();
    while r.left() >= 5 {
        let (kind, v) = (r.u8(), r.u32());
        let i = (v >> 29) as usize;
        let j = ((v >> 26) & 7) as usize;
        match kind & 3 {
            0 => data[i] = data[i].wrapping_add(v) ^ RO_WORDS[j],
            1 => bss[i] = bss[i].wrapping_add(v ^ RO_WORDS[j]),
            2 => bytes[(v & 15) as usize] ^= (v >> 8) as u8,
            _ => core::mem::swap(&mut data[i], &mut bss[j]),
        }
    }
    for v in data.iter() {
        w.u32(*v);
    }
    w.bytes(bytes);
    for v in bss.iter() {
        w.u32(*v);
    }
    for v in RO_WORDS.iter() {
        w.u32(*v);
    }
}

/// OP_HEAP. Payload: at most 64 records of align (u8, a power of two as an
/// exponent), size (u32).
fn heap(r: &mut Reader, w: &mut Writer) {
    let mut blocks = [(0usize, 0usize); 64];
    let mut count = 0;
    while r.left() >= 5 && count < blocks.len() {
        let (align, size) = (1usize << (r.u8() & 7), r.u32() as usize);
        let Some(offset) = heap_alloc(size, align) else {
            w.u32(u32::MAX);
            continue;
        };
        let block = &mut HEAP.get().0[offset..offset + size];
        // A new block is zero, because nothing has written to it.
        let nonzero = block.iter().filter(|b| **b != 0).count();
        for (k, byte) in block.iter_mut().enumerate() {
            *byte = (k as u32).wrapping_mul(31).wrapping_add(offset as u32) as u8;
        }
        w.u32(offset as u32);
        w.u32(nonzero as u32);
        blocks[count] = (offset, size);
        count += 1;
    }
    let mut sum: u64 = 0;
    for &(offset, size) in blocks[..count].iter().rev() {
        for (k, byte) in HEAP.get().0[offset..offset + size].iter().enumerate() {
            sum = sum.rotate_left(1) ^ (*byte as u64).wrapping_mul(k as u64 + 1);
        }
    }
    w.u64(sum);
}

/// OP_ATOMIC. Payload: records of kind (u8), v (u32), x (u32).
fn atomic(r: &mut Reader, w: &mut Writer) {
    const ORD: Ordering = Ordering::SeqCst;
    while r.left() >= 9 {
        let (kind, v, x) = (r.u8(), r.u32(), r.u32());
        match kind {
            0 => w.u32(ATOM_U.fetch_add(v, ORD)),
            1 => w.u32(ATOM_U.fetch_sub(v, ORD)),
            2 => w.u32(ATOM_U.swap(v, ORD)),
            3 => w.u32(ATOM_U.fetch_and(v, ORD)),
            4 => w.u32(ATOM_U.fetch_or(v, ORD)),
            5 => w.u32(ATOM_U.fetch_xor(v, ORD)),
            6 => w.u32(ATOM_U.fetch_max(v, ORD)),
            7 => w.u32(ATOM_U.fetch_min(v, ORD)),
            8 => w.u32(ATOM_I.fetch_max(v as i32, ORD) as u32),
            9 => w.u32(ATOM_I.fetch_min(v as i32, ORD) as u32),
            10 => match ATOM_U.compare_exchange(v, x, ORD, ORD) {
                Ok(old) => {
                    w.u8(1);
                    w.u32(old);
                }
                Err(old) => {
                    w.u8(0);
                    w.u32(old);
                }
            },
            11 => {
                // A weak exchange in a loop: lr.w and sc.w.
                let mut cur = ATOM_U.load(ORD);
                loop {
                    let new = cur.wrapping_mul(3).wrapping_add(v);
                    match ATOM_U.compare_exchange_weak(cur, new, ORD, ORD) {
                        Ok(old) => {
                            w.u32(old);
                            break;
                        }
                        Err(now) => cur = now,
                    }
                }
            }
            12 => w.u32(ATOM_I.fetch_add(v as i32, ORD) as u32),
            13 => w.u32(ATOM_U.load(ORD)),
            _ => halt(EXIT_BAD_PAYLOAD),
        }
    }
}

/// Executes one instruction of the M extension on two registers.
macro_rules! raw_m {
    ($name:literal, $a:expr, $b:expr) => {{
        let out: u32;
        unsafe {
            asm!(
                concat!($name, " {out}, {a}, {b}"),
                out = lateout(reg) out,
                a = in(reg) $a,
                b = in(reg) $b,
                options(pure, nomem, nostack),
            );
        }
        out
    }};
}

/// OP_RAW_M. Payload: records of a (u32), b (u32). The compiler never
/// divides by zero and never divides the least integer by -1; this operation
/// does.
fn raw_m(r: &mut Reader, w: &mut Writer) {
    while r.left() >= 8 {
        let (a, b) = (r.u32(), r.u32());
        w.u32(raw_m!("mul", a, b));
        w.u32(raw_m!("mulh", a, b));
        w.u32(raw_m!("mulhsu", a, b));
        w.u32(raw_m!("mulhu", a, b));
        w.u32(raw_m!("div", a, b));
        w.u32(raw_m!("divu", a, b));
        w.u32(raw_m!("rem", a, b));
        w.u32(raw_m!("remu", a, b));
    }
}

/// Serves requests until the input ends.
fn serve() -> u32 {
    loop {
        let mut header = [0u8; 5];
        match read_full(&mut header) {
            0 => return 0,
            5 => {}
            _ => return EXIT_SHORT_HEADER,
        }
        let len = u32::from_le_bytes([header[1], header[2], header[3], header[4]]) as usize;
        if len > REQ_MAX {
            return EXIT_LONG_PAYLOAD;
        }
        let payload = &mut REQ.get()[..len];
        if read_full(payload) != len {
            return EXIT_SHORT_PAYLOAD;
        }
        let mut r = Reader { buf: payload };
        let mut w = Writer::new();
        match header[0] {
            OP_ARITH64 => arith64(&mut r, &mut w),
            OP_ARITH128 => arith128(&mut r, &mut w),
            OP_SHA256 => w.bytes(&sha256::sha256(r.buf)),
            OP_MEM => mem(&mut r, &mut w),
            OP_RECURSE => recurse(&mut r, &mut w),
            OP_FNTABLE => fntable(&mut r, &mut w),
            OP_STATICS => statics(&mut r, &mut w),
            OP_HEAP => heap(&mut r, &mut w),
            OP_ATOMIC => atomic(&mut r, &mut w),
            OP_EXIT => return r.u32(),
            OP_RAW_M => raw_m(&mut r, &mut w),
            _ => return EXIT_UNKNOWN_OP,
        }
        w.flush();
    }
}

/// The entry point. The machine has set the stack pointer.
#[no_mangle]
pub extern "C" fn _start() -> ! {
    halt(serve())
}
