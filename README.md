# Clearway

A design for automated air traffic control in the United States on one software stack, and a prototype of its safety kernel.

A clearway is the obstacle-free area beyond the end of a runway, kept clear so that a departing aircraft can climb over it. The project keeps the way ahead of every flight clear in the same sense: by guarantee, before the aircraft gets there.

- [docs/design.md](docs/design.md): architecture, safety approach, and migration path.
- [docs/spec.md](docs/spec.md): the kernel specification, in integers only. A Verifier or a whole kernel can be built from it in any language.
- [conformance/](conformance/README.md): 628 test vectors that check a Verifier against the specification, and two recorded vectors that hold the kernel to the same bits on every machine.
- [docs/record.md](docs/record.md): the record of what a kernel decided, chained by hash and signed, and how a party outside the operator checks it.
- [docs/vm.md](docs/vm.md): the one program that is the kernel, and the small machine that runs it, so that every party computes the same bits.
- [docs/replication.md](docs/replication.md): four replicas of the kernel that agree through CometBFT, so that the cell survives a replica that stops and a replica that gives wrong answers.
- [docs/prototype.md](docs/prototype.md): what the prototype contains, how it is checked, and what the simulations show.
- [docs/gap-analysis.md](docs/gap-analysis.md): what separates the prototype from a system that could carry traffic.
- [docs/pitch.md](docs/pitch.md): the pitch we want to be able to make. It is a target, and nothing in it has happened yet.
- [docs/plan.md](docs/plan.md): a three-year plan worked back from that pitch, and the pitch that can be earned by November 2029.

The design in one paragraph: each flight has one cleared 4D trajectory, called a contract. A small, formally specified kernel is the only path by which a clearance can reach an aircraft, and it commits a clearance only if all contracts stay separated. AI plans, predicts, listens, and speaks, and the kernel checks every result. Clearance authority moves from controllers to the automation one airspace cell at a time, through evidence gates.

## Prototype

Go 1.26. The root module uses the standard library only. The replicated cell is a module of its own, `cell/`, which depends on CometBFT. The kernel computes with integers only. It exists twice: in Go, for the simulator, and in Rust (`guest/`), built as one program for a small RISC-V machine that is also in this repository. The built program is in the repository, so the Go tests need no Rust toolchain.

```bash
go test ./...
```

```bash
go run ./cmd/atcsim
```

```bash
go run ./cmd/atcsim -exp all -hours 24 -seeds 8
```

Check a Verifier against the specification:

```bash
go run ./cmd/conform -builtin analytic
```

Write the record of a run, signed with a new key, and check it:

```bash
go run ./cmd/record keygen -out cell
```

```bash
go run ./cmd/atcsim -hours 2 -record run.rec -key cell.key
```

```bash
go run ./cmd/record verify -key cell.pub run.rec
```

Check a published record with the kernel program, in the machine:

```bash
go run ./cmd/record verify -vm conformance/kernel-traffic-0.2.rec.gz
```

Replay the record of that run, `run.rec`, on four replicas that agree through CometBFT, and write the record that they sign together:

```bash
cd cell && go run ./cmd/cell run -record ../run.rec -out ../cell.rec
```

Run a simulation whose every verdict comes from the four replicas, and compare it with the same run with no cell:

```bash
cd cell && go run ./cmd/cell sim -hours 0.5
```

The tests of the cell are separate:

```bash
cd cell && go test ./...
```

Watch a scenario in the browser, at <http://127.0.0.1:8787>:

```bash
go run ./cmd/atcviz
```

| Path | Content |
|---|---|
| `internal/kernel` | Contract Store, Clearance Verifier (two implementations), Conformance Monitor. Integers only, with `internal/fx` and `internal/traj` |
| `guest/` | The same kernel in Rust, with no runtime, and the program that is built from it |
| `internal/rvm`, `internal/kimage`, `cmd/rvgen` | The machine that runs the kernel program: an interpreter, and a translator to Go code that is held to it. And the built program |
| `internal/safetynet` | Tactical Safety Net, independent of the kernel |
| `internal/planner` | Heuristic planner, Fallback Resolver, and a hostile planner for tests |
| `internal/sim` | Fast-time simulation of one en route cell |
| `cmd/atcsim` | Command-line tool and experiment runner |
| `internal/viz`, `cmd/atcviz` | Viewer: replays a scenario on a map, with the kernel's verdicts. `atcviz -export DIR` writes it as files for a web host |
| `internal/conformance`, `cmd/conform` | The conformance suite for Verifier implementations, and its runner |
| `internal/record`, `cmd/record` | The record of a kernel: writer, checker with replay, and command |
| `cell/` | The replicated cell: the kernel program as a CometBFT application, one replica, the client with the certificate of a verdict, and the command. A module of its own |

## Status

This is a research prototype and an independent design study.

- It is not certified. It must not be used to separate real aircraft or to make any operational decision.
- It is not an FAA product, and it has no connection with the FAA.
- The licence provides it with no warranty.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) lists the contributions that would help most and the rules for a change. A second Verifier by another author heads the list. Report a safety defect or a security weakness as [SECURITY.md](SECURITY.md) describes.

## Licence

Apache License 2.0, for the code and the documents. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
