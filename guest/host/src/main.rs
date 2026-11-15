//! The kernel as a native program, for development: it reads the input
//! stream on standard input and writes the answers to standard output. The
//! program that counts is the one in `rv`, which runs in the machine.

use clearway_kernel::{run, Input, Output};
use std::io::{Read, Write};

struct In(std::io::BufReader<std::io::Stdin>);
struct Out(std::io::BufWriter<std::io::Stdout>);

impl Input for In {
    fn read(&mut self, buf: &mut [u8]) -> bool {
        self.0.read_exact(buf).is_ok()
    }
}

impl Output for Out {
    fn write(&mut self, buf: &[u8]) {
        self.0.write_all(buf).expect("write");
    }
}

fn main() {
    let mut input = In(std::io::BufReader::new(std::io::stdin()));
    let mut out = Out(std::io::BufWriter::new(std::io::stdout()));
    let code = match run(&mut input, &mut out) {
        Ok(()) => 0,
        Err(e) => e as i32,
    };
    out.0.flush().expect("flush");
    std::process::exit(code);
}
