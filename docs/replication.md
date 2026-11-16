# The replicated cell

**Status: prototype. Four replicas run on one computer. Nothing here has run across sites.**

The design gives the kernel of each cell to four replicas at four sites, and lets a clearance reach an aircraft only with the signatures of three of them ([design.md](design.md), section 9.5). This document describes the part of that which the prototype now has: four replicas of the kernel that agree through [CometBFT](https://github.com/cometbft/cometbft), a record that they sign together, and a certificate for each verdict that a party outside the cell checks with public keys alone.

**Words.** A *replica* is one computer's copy of the kernel of a cell, with the consensus software around it. The *cell* is the set of replicas. A replica is *correct* if it runs the software as written, and *faulty* if it does anything else: it stops, it answers late, or it gives wrong answers. A *quorum* is more than two thirds of the replicas: three of four. A *batch* is the inputs of the kernel for one instant. A *block* is what the replicas agree on in one step: batches and signatures, in an order.

## 1. What replication is for, and what kind

One kernel on one computer has two weaknesses. The computer can stop, and then the cell stops. The computer can be wrong (a fault in its memory, a defect in its processor, an intruder on its host), and then the cell gives a wrong verdict and no one sees it.

Replication that tolerates only a replica that stops removes the first weakness and keeps the second: every replica must still be trusted to compute right. The design chose the stronger kind, which tolerates a replica that fails in any way (a *Byzantine* fault). It costs one more replica: four tolerate one faulty replica, where three would tolerate one that stops.

| Replicas | Faulty replicas tolerated | Quorum |
|---|---|---|
| 4 | 1 | 3 |
| 7 | 2 | 5 |
| 3f + 1 | f | 2f + 1 |

Two properties follow, and the tests of section 7 show both.

- **Safety.** If at most one replica of four is faulty, the cell gives no verdict that a correct kernel would not give, and no two parties see different verdicts for one input. This holds on any network, however slow.
- **Liveness.** If at least three replicas are correct and can reach one another, the cell answers. With two replicas, the cell gives no answer. It does not give a wrong one. This is principle P5 of the design: consistency before availability.

**Why this engine.** CometBFT is the Tendermint algorithm, of the family of PBFT (Castro and Liskov, 1999): a proposer proposes a block, the replicas vote in two rounds, and a block that gets a quorum in both is final. There is no fork and no later reversal, so a verdict is final when its block is. The engine is in Go, as the prototype is, it is used in production by others, and its interface puts the application (here, the kernel) behind a few calls. Other engines of the same kind would serve. A replication that tolerates only stopped replicas, with a signature of three of four on each checkpoint beside it, is simpler and proves less at the moment of a verdict.

## 2. The parts

The cell is a Go module of its own, `cell/`, inside the repository. The root module keeps its rule of no dependency. The cell module depends on CometBFT 1.0.1 and on what that needs, and it uses the kernel program, the machine, and the record of the root module.

| Package | Content |
|---|---|
| `cell/app` | The kernel as an application of CometBFT: transactions, blocks, the application hash, and the record that a replica builds |
| `cell/node` | One replica: a CometBFT node with the application, and the signer of checkpoints. And the files of a cell |
| `cell/client` | What a party outside the cell uses: it gives inputs, reads verdicts, and gets and checks certificates |
| `cell/cmd/cell` | The command |

**Each replica runs the kernel program.** A replica holds the kernel as the one published program of [vm.md](vm.md), in the machine, and gives it one message at a time (`kimage.Process`). So every replica computes the same bits, and they are the bits that a party computes who replays the record. A replica can run the program as translated code or in the interpreter alone. In the tests, one replica of four uses the interpreter, and it agrees with the others at every block.

## 3. How the cell applies an input

1. A client gives a *batch* to one replica: the input lines of the record ([record.md](record.md), section 2.2) for one instant, with the number of the batch. The batches of a cell are numbered from 0.
2. The replica checks the form of the batch and passes it to the others.
3. One replica proposes a block. It puts the batches that it holds in the order of their numbers, from the next number, with no gap.
4. Each replica checks the proposed block. A replica refuses a block in which the batches are not the next ones in order, or in which a line is not an input line. So a faulty proposer cannot change the order of the inputs or leave one out in the middle.
5. The replicas vote. A block with the votes of a quorum is final.
6. Each replica gives the lines of the block to its kernel, in order. The kernel answers each proposal with a verdict line. The verdict lines of a batch are the *result* of the batch.
7. Each replica computes the *application hash*: the SHA-256 of the checkpoint line of the record after this block, with no signature. The checkpoint line states the cell, the number of chained lines, the kernel time, the chain value over every line, and the state digest of the kernel.
8. The header of the next block holds the application hash and a hash of the results. The replicas vote on that header. A replica whose kernel reached another state, or gave another verdict, has another hash, and cannot agree.

Step 8 is where a wrong replica shows. The replicas do not vote on verdicts one at a time. They vote on one value that fixes every line of the record so far, every verdict, and the whole state of the kernel.

The cell makes a block when it has a batch or a signature, and one more block after a block that changed the state, which carries the new hash. It makes no block when nothing arrives.

**The kernel time comes from the inputs.** A line of the record moves the kernel clock. The time that CometBFT puts in a block is not an input to the kernel, so the clocks of the replicas cannot make them differ.

**If the kernel program stops.** The program stops on a defect, for example an overflow or a full heap ([vm.md](vm.md), section 4.4). Every correct replica then stops at the same block, and the cell stops. The replicas do not diverge.

### 3.1 Transactions

A transaction is one of two kinds, by its first byte.

| First byte | Content |
|---|---|
| `I` | The number of the batch in decimal, a line feed, and the input lines of the batch with a line feed between them |
| `S` | A JSON object with the fields `seq`, `key`, and `sig`: the signature of one replica for the checkpoint after chained line `seq` (section 4) |

| Result code | Meaning |
|---|---|
| 0 | Applied |
| 1 | Not a transaction of the cell, or a line that is not an input line |
| 2 | A batch that is not the next one |
| 3 | A signature that does not count: not the key of a replica, not a checkpoint that the replicas sign, not valid, or a second one from one replica |

A transaction with a code other than 0 changes nothing.

## 4. The record of the cell

Each replica builds the record of [record.md](record.md) as it applies blocks: the header line, each input line, and each verdict line, in order. The chain value of that record is in the application hash, so the replicas agree on every line of it.

The replicas also sign checkpoints of the record, so that a party can check the record with no knowledge of CometBFT.

- At a fixed interval of kernel time, or after each block that has an input, the application marks the checkpoint after the block as one to sign.
- Each replica signs it with its Ed25519 key, in the form of [record.md](record.md), section 4, and sends the signature to the cell as a transaction.
- A later block holds the signatures. The application takes one signature for each replica, and only a valid one.
- The record that a replica writes has each checkpoint with its signatures, after the line that it covers.

A party then checks the record of a cell exactly as it checks the record of one kernel, with the public keys of the replicas and a quorum:

```bash
go run ./cmd/record verify -quorum 3 -key KEY1 -key KEY2 -key KEY3 -key KEY4 cell.rec
```

The check replays every input in a kernel of its own. So it shows two things: three of the four replicas signed this history, and the verdicts in it are the verdicts of the published kernel.

A replica signs its votes and its checkpoints with one key. The genesis file of the cell names the keys.

## 5. The certificate of a verdict

The design lets the Gateway at a radio site deliver a clearance only with a *quorum certificate*: the signatures of the replicas that committed it. `cell/client` makes that certificate and checks it.

A certificate for one batch holds these parts.

| Part | What it shows |
|---|---|
| The header of the block that holds the batch | It holds a hash of the transactions of the block |
| The batch, with a Merkle proof | The block holds this batch at this place |
| The header of the next block | It holds the hash of the first header, and a hash of the results of the first block |
| The result of the batch, with a Merkle proof | The results of the first block have these verdict lines at the place of the batch |
| The signatures of the replicas for the next header | A quorum agreed on all of the above |

The check needs the name of the chain of the cell and the public keys of the replicas, which are in the genesis file. It needs no replica and no network. It returns the number of the batch, its input lines, and its verdict lines. A certificate for a batch of the test record is about 5,400 bytes of JSON.

**What a faulty replica can do.** One faulty replica can refuse to give a certificate. It cannot make one: a certificate that it makes up has one signature, and the check needs three. Two faulty replicas cannot make one. Three can, and the check cannot tell their certificate from a true one. That is the limit of a cell of four.

## 6. To run it

Four replicas in one process replay the record of a simulation, and write the record of the cell:

```bash
go run ./cmd/atcsim -hours 0.5 -record run.rec
```

```bash
cd cell && go run ./cmd/cell run -record ../run.rec -out ../cell.rec
```

The command prints the line that checks `cell.rec` with `record verify`.

Each replica as a process of its own:

```bash
cd cell && go run ./cmd/cell setup -dir /tmp/cell -record ../conformance/kernel-session-0.2.rec
```

```bash
cd cell && go run ./cmd/cell node -home /tmp/cell/replica0 -out /tmp/cell/cell.rec
```

Start `replica1`, `replica2`, and `replica3` in the same way, each in its own terminal. Then give the inputs of the record to the cell through one of them:

```bash
cd cell && go run ./cmd/cell feed -home /tmp/cell/replica1 -record ../conformance/kernel-session-0.2.rec
```

Stop one replica, and `cell feed` on a new cell still passes. Stop two, and it waits. A replica that is interrupted with Ctrl-C writes the record of the cell to the file of `-out`.

## 7. How it is tested

Every test uses real CometBFT nodes that talk over the network of one computer. The inputs are those of the two published records of [conformance/](../conformance/README.md), and the expected verdicts and state digests are the ones in those records.

| Test | What it shows |
|---|---|
| `TestCell` | Four replicas, one of them with the interpreter alone. Each step of the session record goes in through another replica. The cell gives the 11 verdicts of the record, and at each of its 16 checkpoints every replica has its number of lines, kernel time, chain value, and state digest. All four then hold the same record, signed by all four, and it passes `record verify` with a quorum of three. |
| `TestCrashedReplicas` | Two of the replicas are processes, and the test kills them (SIGKILL). With three replicas the cell goes on. With two it makes no block and gives no verdict. When the third comes back, the batch that waited gets its block. A replica that comes back applies every block from the first, gets the blocks that it missed from the others, and then has their state. The record passes the check. |
| `TestLyingReplica` | From block 3, one replica reports a state and verdicts that it does not have. The other three do not agree with it. The cell gives the verdicts of the record, and its record passes the check. The faulty replica stops: it sees that the cell agreed on a block that its own state does not allow. |
| `TestCertificate` | A party gets certificates from a replica that did not take the inputs, as JSON, and checks them with the genesis file. They give the batches and the verdicts of the record. Nine forgeries fail: another verdict, the verdict of another batch, another batch, another block, signatures for another block, another chain, the replicas of another cell, and a verdict that one replica or two made up and signed. A forgery by three replicas passes, as section 5 says. |
| `cell/app` | The application without a network. Two replicas, one with the interpreter alone, have the same application hash after every block. Signatures: of three replicas, a second one from one replica, one from a key that is no replica's, and one for another statement. Ten kinds of bad transaction change nothing. A proposer puts batches in order, and a replica refuses a block that has them out of order, with a gap, or with a line that is not an input. |
| `TestRun` | `cell run` on the session record and on the traffic record: 30 minutes of simulated traffic at 240 flights an hour, with a hostile planner. 1,800 batches, 428 verdicts, and 30 state digests equal those of the record. |
| `TestProcesses` | `cell setup`, four `cell node` processes, and `cell feed` through one of them. A replica that is interrupted writes a record that passes the check. |

The tests of the two modules are separate:

```bash
cd cell && go test ./...
```

**Measurements.** One 2025 laptop, four replicas on one disk.

| | |
|---|---|
| The traffic record through four replicas | 1,800 batches in about 140 blocks, in 22 s. That is about 80 times as fast as the traffic itself |
| One block | About 0.16 s. A batch that arrives alone waits that long for its verdict |
| One durable write on this disk | 4.3 ms |

The kernel is not what takes the time: the kernel program applies the whole traffic record in 0.4 s. A block takes many durable writes on each replica (the log of the consensus, the state of the signer, the block, the state), and the four replicas share one disk. I did not count the writes of one block, so this explanation is likely and not shown. Nothing is tuned.

## 8. What the work found in CometBFT

These are facts about CometBFT 1.0.1 that the code works around or the tests rely on.

- **A replica that has just started takes no input.** It first gets the blocks that it lacks. `client.Ready` waits for the end of that.
- **The results of the newest block come a moment after the block.** A client that reads blocks as they appear must wait for the results. `client.Feed` does.
- **A faulty replica is not ended.** When a replica sees a quorum for a block that its own state does not allow, CometBFT logs `CONSENSUS FAILURE` and stops the consensus of that replica. The process stays, and takes no further part.
- **A node that stops inside a process that goes on is not clean.** It closes its databases while one of its own goroutines can still read them, and that read ends the process. It can also leave a connection to another replica open, and that replica then refuses the node when it starts again. So a replica keeps its databases open until its process ends, and a replica starts again only in a new process. A process that is killed has neither problem, and that is the case that the crash test uses.

## 9. What is not done

- **Inputs are not authenticated.** Any party that can reach a replica can give it a batch. The number of a batch fixes the order and not the origin. The sources of the design (surveillance, the planner, the Gateway) must sign what they send, and the application must check it. Until then the cell protects against a faulty replica and not against a false input, which is principle P6 of the design.
- **One feeder.** The numbers of the batches assume one source that knows the next number. Two sources need a rule: a sequencer, or a number for each source.
- **All replicas are on one computer.** Nothing was run across sites, with real delay, loss, or a partition of the network. The tests have crashed replicas and one lying replica. They have no replica that is slow, no replica that sends different messages to different replicas at the level of the network, and no faulty proposer on the network: the order rule of section 3 is tested in the application alone.
- **A replica that comes back applies every block from the first.** That takes longer as the history grows. A replica should start from a recent state instead. The machine state digest of [vm.md](vm.md) names such a state, and nothing stores or sends one.
- **The record is in memory.** A replica rebuilds it from the blocks when it starts. A replica should write it to a file as it goes.
- **A signature must arrive within 64 checkpoints.** The application takes signatures for the newest 64 checkpoints that the replicas sign. A replica that was away for longer does not sign the older ones. The checkpoints still have the signatures of the replicas that ran.
- **The set of replicas is fixed.** The genesis file names the replicas. Nothing adds or removes one, and nothing replaces a key.
- **One key for two uses.** A replica signs votes and checkpoints with one key. With two keys, the key that signs the record could be kept apart from the consensus software.
- **The certificate has no format of its own.** It is JSON of CometBFT's types. A Gateway in another language needs a stated format and a second implementation of the check.
- **The Gateway is not built.** `cell/client` is the check that a Gateway would make. Nothing delivers a clearance.
- **The simulator does not use the cell.** It runs the Go kernel in its own process and writes a record. The cell replays that record. A simulation that puts each input through the cell, and waits for the verdict, is the next step.
- **The delay of a block is not tuned.** A verdict in 0.16 s is enough for strategic clearances, which are planned minutes ahead. It was measured on one disk, and not across sites, where the delay of the network adds to each of the three steps of a block.
- **CometBFT is large.** A replica trusts it, and the 67 other modules that `cell/go.mod` lists. A party that checks a record or a certificate trusts less: the record needs the root module alone, and the certificate needs CometBFT's types and signature check.
- **No island mode.** The design lets an edge node take limited authority when it loses the core sites (section 9.2). A cell without a quorum here only waits.
