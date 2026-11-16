# Contributing

Clearway is a design for automated air traffic control and a prototype of its safety kernel. Read [docs/design.md](docs/design.md) for the design and [docs/gap-analysis.md](docs/gap-analysis.md) for what is missing. The gap analysis is the list of work.

## Contributions that would help most

- **A Verifier implementation by another author.** One author wrote both of the current ones. A third is the most valuable single contribution. Write it from [docs/spec.md](docs/spec.md), in any language, without reading `internal/kernel` or `guest/kernel`. Check it with the [conformance suite](conformance/README.md).
- **A kernel by another author.** The specification now defines every bit of a verdict and of a state digest. A kernel that is written from it alone and that replays the two kernel vectors would show that the specification is complete. [docs/vm.md](docs/vm.md), section 4, defines the streams to drive it with.
- **A second build of the kernel program, or a second machine.** The same bytes from the same source on another computer, with a pinned toolchain. Or a machine by another author that runs the program to the same answers ([docs/vm.md](docs/vm.md)).
- **A planner.** Any planner can be tried, because the kernel decides. Report what `go run ./cmd/atcsim -exp planners` shows with yours.
- **Data on how real aircraft hold an assigned trajectory.** This is assumption A2, and the prototype takes it on trust.
- **A formal specification** of the kernel invariants.
- **A terminal cell**: closed arrival contracts and runway reservations.
- **A defect.** A failing test is a contribution on its own.

## Before you send a change

Run what the continuous integration runs.

```bash
test -z "$(gofmt -l .)"
```

```bash
go vet ./...
```

```bash
go test ./...
```

The replicated cell is a module of its own, with its own tests. They start CometBFT nodes on the network of this computer and take about two minutes:

```bash
cd cell && go vet ./... && go test ./...
```

The Go tests need no Rust toolchain: the built kernel program is in the repository. A change under `guest/` needs one, with the target `riscv32imac-unknown-none-elf`. Build the kernel as a native program, so that `go test ./internal/record` compares it with the Go kernel on long records too:

```bash
cargo build --release --manifest-path guest/Cargo.toml -p clearway-kernel-host
```

Then build the kernel program for the machine. The script also writes the translated code of the program, `internal/kimage/kcode.go`, and prints the SHA-256 of the program, which goes into `internal/kimage/kimage.go`:

```bash
./guest/build.sh
```

A change to the translator or to the decoder of `internal/rvm` changes the translated code. Write it again for the kernel program and for the two test programs of the machine:

```bash
go run ./cmd/rvgen -pkg kimage -name kernelCode -o internal/kimage/kcode.go internal/kimage/kernel.elf
```

```bash
RVM_WRITE_CODE=1 go test -tags rvmbootstrap ./internal/rvm -run TestTranslationsAreCurrent
```

## Rules

1. **The kernel stays deterministic and alone.** `internal/kernel` imports only `internal/fx` and `internal/traj`. It uses no random numbers and no clock. `internal/safetynet` imports nothing from this module. The two Verifier implementations do not call each other's geometry. The tests in `internal/arch` enforce this.
   The kernel must compute the same bits on every machine. So `internal/fx`, `internal/traj`, `internal/kernel`, and `guest/kernel` have no floating point: no such type, no such literal, and no use of package `math` ([docs/spec.md](docs/spec.md), section 1). `guest/kernel` also has no unsafe code. `internal/arch` tests these rules too.
2. **No dependencies.** The root module uses the Go standard library only, and `guest/` uses no crate. A change that adds a dependency must say why nothing else will do. The one exception is the module `cell/`, which holds the replicated cell and depends on CometBFT. Nothing in the root module imports it, so the kernel, the machine, the record, and the check of a record stay free of dependencies.
3. **The two kernels move together.** A change to what the kernel does changes `internal/kernel` and `guest/kernel` in one commit, with the kernel program built again, its translated code, and its new hash in `internal/kimage/kimage.go`. `go test ./internal/kimage ./internal/record` then shows that the two give the same bits.
4. **The interpreter defines the machine.** Translated code is a faster way to the same states, and nothing else. A change to `rvm.Translate` must keep every test that compares translated code with the interpreter, and a new kind of translated instruction needs a slot in the program of `opsProgram`.
5. **A change to the kernel or to a Verifier carries its evidence.**
   - A test that fails without the change.
   - Both implementations still agree with the brute-force oracle, still report every collision course, and still pass the conformance suite.
   - `go run ./cmd/atcsim -exp verifiers` still shows no loss of the kernel class and no alarm for two correct implementations. Each implementation must also pass alone: a pair can hide a defect in one member.
   - If the change alters what the kernel does, the two kernel vectors are written again ([conformance/README.md](conformance/README.md)). If it does not, they pass as they are.
6. **A fix for a defect names the defect.** Say in the code or in its test why the old form was wrong, as `TestSampledStaysInsideTheTolerance` in `internal/kernel` does.
7. **The specification and the suite move together.** A change to the meaning of a tube or of separation changes [docs/spec.md](docs/spec.md), gives the vectors a new version, and must leave every faulty Verifier in `internal/conformance` failing.
8. **A scenario says only what it does.** `TestScenariosShowTheirPoint` holds every scenario description in the viewer to the run. Change both together.
9. **Published numbers come from one command.** The tables in [docs/prototype.md](docs/prototype.md) come from `go run ./cmd/atcsim -exp all -hours 24 -seeds 8`. If your change moves them, run it again and update the tables.
10. **Documents use plain, exact language.** Short active sentences. One term for one concept; the terms are in section 5 of the design. A dagger (†) marks a value that simulation must confirm.

## Licence of contributions

The project is under the Apache License 2.0. Under section 5 of that licence, a contribution that you submit is under the same licence. There is no separate agreement to sign.

## Safety and security reports

See [SECURITY.md](SECURITY.md).
