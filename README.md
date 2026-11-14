# auto-atc

A design for automated air traffic control in the United States on one software stack, and a prototype of its safety kernel.

- [docs/design.md](docs/design.md): architecture, safety approach, and migration path.
- [docs/prototype.md](docs/prototype.md): what the prototype contains, how it is checked, and what the simulations show.

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

This is an independent design study. It is not an FAA document.
