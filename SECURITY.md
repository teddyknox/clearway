# Security

This repository is a research prototype. No part of it is deployed, and it must not be used to control aircraft. A weakness here puts no aircraft at risk. Reports are still wanted: the design assumes that an adversary has read the source, and a report tests that assumption.

## Two kinds of report

**A safety defect in the kernel.** A case in which the kernel commits a clearance that breaks an invariant, a Verifier implementation approves a pair that is not separated, or the Monitor misses an aircraft that left its envelope.

- Open a public issue. Nothing is deployed, so nothing needs an embargo.
- Add a failing test if you can. The brute-force oracle in `internal/kernel/checkers_test.go` is the reference.

**A security weakness in the tools.** For example a way to make the viewer server read a file outside its directory, or run code.

- Report it in private with the "Report a vulnerability" button under the Security tab of the repository.
- Say what you did, what happened, and which commit you used.

## What to expect

Volunteers maintain this project. There is no guaranteed response time. A confirmed report is fixed in the open, with a test, and the reporter is credited unless they ask not to be.

## Notes for people who run the code

- `cmd/atcviz` listens on `127.0.0.1` by default and has no authentication. Do not expose it to a network.
- The module has no dependencies outside the Go standard library.
