//! The Clearway kernel, for the RISC-V machine of `internal/rvm`.
//!
//! This is the kernel of `internal/kernel`, written a second time: the
//! Contract Store, the two Verifier implementations, and the Conformance
//! Monitor. It has no runtime, no dependency, and no floating point. Every
//! quantity is an integer: micrometres, microseconds, micrometres per second.
//!
//! A host feeds it a stream of inputs and reads a stream of answers (`run`).
//! The same inputs must give the same verdicts and the same state digests as
//! the Go kernel, bit for bit. `docs/vm.md` defines the machine and the two
//! streams.
#![no_std]
#![forbid(unsafe_code)]

extern crate alloc;

pub mod fx;
pub mod kernel;
pub mod sha256;
pub mod traj;
pub mod verifier;

use alloc::vec::Vec;
use fx::Vec2;
use kernel::{Authority, Kernel, Params, Track, Verdict};
use traj::{Leg, Trajectory, VSeg};
use verifier::Arrangement;

/// A source of input bytes. `read` fills the whole buffer, or returns false
/// at the end of the input.
pub trait Input {
    fn read(&mut self, buf: &mut [u8]) -> bool;
}

/// A sink for output bytes.
pub trait Output {
    fn write(&mut self, buf: &[u8]);
}

/// Why `run` stopped before the end of the input. The number is the exit code
/// of the program.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Error {
    /// The input ended inside a message, or does not start with the header.
    Truncated = 2,
    /// The header is for another version of the stream.
    Version = 3,
    /// A message has a kind or a value that the stream does not define.
    Malformed = 4,
}

/// The first eight bytes of an input stream: "CLWK", and the version 2.
pub const MAGIC: [u8; 8] = *b"CLWK\x02\x00\x00\x00";

// The kinds of message. 0 to 12 are the inputs of the kernel, with the
// numbers of `InputKind` in the Go kernel.
const ADVANCE: u8 = 0;
const ADMIT: u8 = 1;
const PROPOSE: u8 = 2;
const RECONTRACT: u8 = 3;
const ACK: u8 = 4;
const REFUSE: u8 = 5;
const FAIL: u8 = 6;
const TRACK: u8 = 7;
const MARK_INTRUDER: u8 = 8;
const REANCHOR: u8 = 9;
const REMOVE: u8 = 10;
const TICK: u8 = 11;
const AUTHORITY: u8 = 12;
/// Asks for the state digest.
const DIGEST: u8 = 250;
/// Ends the stream.
const END: u8 = 255;

// The kinds of answer.
const OUT_VERDICT: u8 = 1;
const OUT_DIGEST: u8 = 2;

struct Reader<'a, I: Input> {
    input: &'a mut I,
}

impl<I: Input> Reader<'_, I> {
    fn bytes<const N: usize>(&mut self) -> Result<[u8; N], Error> {
        let mut b = [0u8; N];
        if self.input.read(&mut b) {
            Ok(b)
        } else {
            Err(Error::Truncated)
        }
    }
    fn u8(&mut self) -> Result<u8, Error> {
        Ok(self.bytes::<1>()?[0])
    }
    fn u32(&mut self) -> Result<u32, Error> {
        Ok(u32::from_le_bytes(self.bytes()?))
    }
    fn i64(&mut self) -> Result<i64, Error> {
        Ok(i64::from_le_bytes(self.bytes()?))
    }
    fn flag(&mut self) -> Result<bool, Error> {
        match self.u8()? {
            0 => Ok(false),
            1 => Ok(true),
            _ => Err(Error::Malformed),
        }
    }
    fn authority(&mut self) -> Result<Authority, Error> {
        match self.u8()? {
            0 => Ok(Authority::Automation),
            1 => Ok(Authority::Human),
            _ => Err(Error::Malformed),
        }
    }
    fn vec2(&mut self) -> Result<Vec2, Error> {
        Ok(Vec2 { x: self.i64()?, y: self.i64()? })
    }

    /// Reads a trajectory. One with more parts than a valid trajectory may
    /// have is read to its end and returned empty: the kernel rejects both.
    fn trajectory(&mut self) -> Result<Trajectory, Error> {
        let mut tr = Trajectory::default();
        let legs = self.u32()? as usize;
        let mut keep = legs <= traj::MAX_PARTS;
        for _ in 0..legs {
            let l = Leg { t0: self.i64()?, t1: self.i64()?, p0: self.vec2()?, v: self.vec2()? };
            if keep {
                tr.legs.push(l);
            }
        }
        let vsegs = self.u32()? as usize;
        keep = keep && vsegs <= traj::MAX_PARTS;
        for _ in 0..vsegs {
            let s = VSeg { t0: self.i64()?, t1: self.i64()?, a0: self.i64()?, a1: self.i64()? };
            if keep {
                tr.vsegs.push(s);
            }
        }
        if !keep {
            tr = Trajectory::default();
        }
        Ok(tr)
    }

    fn params(&mut self) -> Result<Params, Error> {
        Ok(Params {
            sh: self.i64()?,
            sv: self.i64()?,
            vtol: self.i64()?,
            horizon: self.i64()?,
            commit_horizon: self.i64()?,
            intruder_horizon: self.i64()?,
            extend_slack: self.i64()?,
            w: self.i64()?,
            e0: self.i64()?,
            bh: self.i64()?,
            k: self.i64()?,
            min_lead: self.i64()?,
            cell_radius: self.i64()?,
            intruder_r0: self.i64()?,
            intruder_k: self.i64()?,
            intruder_vu: self.i64()?,
            recontract_tol: self.i64()?,
            intent_check: self.flag()?,
            intent_lookahead: self.i64()?,
        })
    }
}

fn verdict<O: Output>(out: &mut O, v: Verdict) {
    let mut msg: Vec<u8> = Vec::with_capacity(19);
    msg.push(OUT_VERDICT);
    msg.push(v.ok as u8);
    if v.ok {
        msg.extend_from_slice(&v.contract.to_le_bytes());
    } else {
        msg.push(v.reason as u8);
        msg.extend_from_slice(&v.with.to_le_bytes());
        msg.extend_from_slice(&v.at.to_le_bytes());
    }
    out.write(&msg);
}

/// Runs one kernel on a stream of inputs.
///
/// The stream starts with MAGIC, the arrangement (0 single, 1 dual), and the
/// parameters. Then each message is one input of the kernel, a request for
/// the state digest, or the end. For each proposal `run` writes a verdict,
/// and for each request a digest.
///
/// The host checks the parameters before it starts the kernel
/// (`Params.Validate` in the Go kernel).
pub fn run<I: Input, O: Output>(input: &mut I, out: &mut O) -> Result<(), Error> {
    let mut r = Reader { input };
    let magic: [u8; 8] = r.bytes()?;
    if magic[..4] != MAGIC[..4] {
        return Err(Error::Truncated);
    }
    if magic != MAGIC {
        return Err(Error::Version);
    }
    let arr = match r.u8()? {
        0 => Arrangement::Single,
        1 => Arrangement::Dual,
        _ => return Err(Error::Malformed),
    };
    let mut k = Kernel::new(r.params()?, arr);
    loop {
        match r.u8()? {
            ADVANCE => k.advance(r.i64()?),
            ADMIT => {
                let id = r.i64()?;
                verdict(out, k.admit(id, r.trajectory()?));
            }
            PROPOSE => {
                let (id, by) = (r.i64()?, r.authority()?);
                verdict(out, k.propose(id, r.trajectory()?, by));
            }
            RECONTRACT => {
                let (id, by) = (r.i64()?, r.authority()?);
                verdict(out, k.propose_recontract(id, r.trajectory()?, by));
            }
            ACK => k.ack(r.i64()?),
            REFUSE => k.refuse(r.i64()?),
            FAIL => k.delivery_failed(r.i64()?),
            TRACK => {
                let id = r.i64()?;
                let tr = Track {
                    t: r.i64()?,
                    pos: r.vec2()?,
                    vel: r.vec2()?,
                    alt: r.i64()?,
                    vrate: r.i64()?,
                    has_sel: r.flag()?,
                    sel_alt: r.i64()?,
                };
                k.update_track(id, tr);
            }
            MARK_INTRUDER => k.mark_intruder_input(r.i64()?),
            REANCHOR => k.reanchor_all(),
            REMOVE => k.remove(r.i64()?),
            TICK => k.tick(),
            AUTHORITY => {
                let id = r.i64()?;
                k.set_authority(id, r.authority()?);
            }
            DIGEST => {
                out.write(&[OUT_DIGEST]);
                out.write(&k.state_digest());
            }
            END => return Ok(()),
            _ => return Err(Error::Malformed),
        }
    }
}
