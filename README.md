# Clearway

A design for automated air traffic control in the United States on one software stack, and a prototype of its safety kernel.

A clearway is the obstacle-free area beyond the end of a runway, kept clear so that a departing aircraft can climb over it. The project keeps the way ahead of every flight clear in the same sense: by guarantee, before the aircraft gets there.

- [docs/design.md](docs/design.md): architecture, safety approach, and migration path.
- [docs/spec.md](docs/spec.md): the kernel specification. A Verifier can be built from it in any language.
- [conformance/](conformance/README.md): 588 test vectors that check a Verifier against the specification.
- [docs/prototype.md](docs/prototype.md): what the prototype contains, how it is checked, and what the simulations show.
- [docs/gap-analysis.md](docs/gap-analysis.md): what separates the prototype from a system that could carry traffic.
- [docs/pitch.md](docs/pitch.md): the pitch we want to be able to make. It is a target, and nothing in it has happened yet.
- [docs/plan.md](docs/plan.md): a three-year plan worked back from that pitch, and the pitch that can be earned by November 2029.

The design in one paragraph: each flight has one cleared 4D trajectory, called a contract. A small, formally specified kernel is the only path by which a clearance can reach an aircraft, and it commits a clearance only if all contracts stay separated. AI plans, predicts, listens, and speaks, and the kernel checks every result. Clearance authority moves from controllers to the automation one airspace cell at a time, through evidence gates.

## Prototype

Go 1.26, standard library only.

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

Watch a scenario in the browser, at <http://127.0.0.1:8787>:

```bash
go run ./cmd/atcviz
```

| Path | Content |
|---|---|
| `internal/kernel` | Contract Store, Clearance Verifier (two implementations), Conformance Monitor |
| `internal/safetynet` | Tactical Safety Net, independent of the kernel |
| `internal/planner` | Heuristic planner, Fallback Resolver, and a hostile planner for tests |
| `internal/sim` | Fast-time simulation of one en route cell |
| `cmd/atcsim` | Command-line tool and experiment runner |
| `cmd/atcviz` | Viewer: replays a scenario on a map, with the kernel's verdicts |
| `internal/conformance`, `cmd/conform` | The conformance suite for Verifier implementations, and its runner |

## Status

This is a research prototype and an independent design study.

- It is not certified. It must not be used to separate real aircraft or to make any operational decision.
- It is not an FAA product, and it has no connection with the FAA.
- The licence provides it with no warranty.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md) lists the contributions that would help most and the rules for a change. A second Verifier by another author heads the list. Report a safety defect or a security weakness as [SECURITY.md](SECURITY.md) describes.

## Licence

Apache License 2.0, for the code and the documents. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
