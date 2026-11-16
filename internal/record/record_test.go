package record

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// Signing keys for tests. They protect nothing.
var (
	keyA = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	keyB = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, ed25519.SeedSize))
)

func pub(k ed25519.PrivateKey) ed25519.PublicKey { return k.Public().(ed25519.PublicKey) }

func trust(keys ...ed25519.PrivateKey) Options {
	var o Options
	for _, k := range keys {
		o.Trusted = append(o.Trusted, pub(k))
	}
	return o
}

// The tests state values in nautical miles, feet, and seconds, and convert
// them once, as a planner does.
const cruise = 480 * geom.Kt

func sec(x float64) int64 { return conv.Time(x) }
func ft(x float64) int64  { return conv.Alt(x) }

func straight(t0 float64, a, b geom.Vec2, alt float64) *traj.Trajectory {
	legs := ftraj.Route(t0, []geom.Vec2{a, b}, []float64{cruise})
	return conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Level(t0, legs[0].T1, alt)})
}

// tail returns an amendment for the active contract of f: from its position
// at td, through via, to end, at the level of the contract.
func tail(f *kernel.Flight, td float64, via, end geom.Vec2) *traj.Trajectory {
	tr := f.Active.Traj
	legs := ftraj.Route(td, []geom.Vec2{conv.PM(tr.Pos(sec(td))), via, end}, []float64{cruise, cruise})
	return conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Level(td, legs[len(legs)-1].T1, conv.Ft(tr.AltAt(sec(td))))})
}

// onContract returns the track of an aircraft that is exactly on tr at t.
func onContract(tr *traj.Trajectory, t int64) kernel.Track {
	at := tr.Pos(t)
	x, _ := fx.Int(at.X + fx.M/2).DivFloor(fx.M)
	y, _ := fx.Int(at.Y + fx.M/2).DivFloor(fx.M)
	return kernel.Track{T: t, Pos: fx.Vec{X: x, Y: y}, Vel: tr.VelAt(t), Alt: tr.AltAt(t)}
}

// session runs a kernel through a fixed script and returns its record. The
// script uses every kind of input, and it gets commits and rejections for
// several reasons. It runs to kernel time until, with a checkpoint at the
// start and after each minute.
//
// With fork set, the script has one more input at 400 s: the history is then
// a different one from that line on.
func session(t testing.TB, until float64, fork bool, signers ...ed25519.PrivateKey) []byte {
	t.Helper()
	var buf bytes.Buffer
	p := kernel.DefaultParams()
	w, err := NewWriter(&buf, Header{Cell: "test-cell", Verifier: "dual", Params: p, Note: "scripted session"}, signers...)
	if err != nil {
		t.Fatal(err)
	}
	d, err := kernel.Arrangement("dual")
	if err != nil {
		t.Fatal(err)
	}
	k := kernel.New(p, d)
	k.Rec = w
	checkpoint := func() { w.Checkpoint(k.Now(), k.StateDigest()) }
	want := func(v kernel.Verdict, ok bool, reason kernel.RejectReason) {
		t.Helper()
		if v.OK != ok || v.Reason != reason {
			t.Fatalf("at %d µs the script got %+v; it needs ok=%v reason=%v", k.Now(), v, ok, reason)
		}
	}
	xy := func(x, y float64) geom.Vec2 { return geom.Vec2{X: x, Y: y} }

	k.Advance(0)
	eastbound := straight(0, xy(-160, 0), xy(160, 0), 35000)
	want(k.Admit(1, eastbound), true, 0)
	want(k.Admit(2, eastbound), false, kernel.RejConflict) // the same route at the same level
	want(k.Admit(2, straight(0, xy(0, -160), xy(0, 160), 36000)), true, 0)
	want(k.Admit(1, eastbound), false, kernel.RejDuplicate)
	want(k.Admit(3, straight(0, xy(160, 20), xy(-160, 20), 37000)), true, 0)
	checkpoint()

	for now := 5.0; now <= until; now += 5 {
		k.Advance(sec(now))
		for _, id := range append([]kernel.FlightID(nil), k.IDs()...) {
			f := k.Flight(id)
			tk := f.Track
			if f.Intruder {
				// An intruder holds its last velocity.
				dx, _ := fx.Mul(tk.Vel.X, sec(now)-tk.T).DivFloor(fx.M)
				dy, _ := fx.Mul(tk.Vel.Y, sec(now)-tk.T).DivFloor(fx.M)
				tk.Pos, tk.T = tk.Pos.Add(fx.Vec{X: dx, Y: dy}), sec(now)
			} else {
				tk = onContract(f.Active.Traj, sec(now))
				if id == 1 {
					tk.HasSel, tk.SelAlt = true, tk.Alt // flight 1 broadcasts its selected altitude
				}
			}
			k.UpdateTrack(id, tk)
		}
		switch now {
		case 60:
			want(k.Propose(1, tail(k.Flight(1), now+150, xy(0, 40), xy(170, 40)), kernel.Automation), true, 0)
		case 65:
			k.Ack(1)
		case 120:
			want(k.Propose(2, tail(k.Flight(2), now+30, xy(40, 0), xy(40, 170)), kernel.Automation), false, kernel.RejLead)
		case 130:
			k.SetAuthority(2, kernel.Human)
			want(k.Propose(2, tail(k.Flight(2), now+150, xy(40, 0), xy(40, 170)), kernel.Automation), false, kernel.RejAuthority)
		case 140:
			want(k.Propose(2, tail(k.Flight(2), now+150, xy(40, 0), xy(40, 170)), kernel.Human), true, 0)
		case 145:
			k.Refuse(2)
		case 200:
			want(k.Propose(3, tail(k.Flight(3), now+150, xy(0, 60), xy(-170, 60)), kernel.Automation), true, 0)
		case 205:
			k.DeliveryFailed(3)
		case 400:
			if fork {
				k.SetAuthority(1, kernel.Human)
			}
		case 500:
			k.MarkIntruder(2)
		case 510:
			tk := k.Flight(2).Track
			start := now + 150
			vel := conv.NMVel(tk.Vel)
			from := conv.NMPoint(tk.Pos).Add(vel.Scale(start - conv.Sec(tk.T)))
			legs := ftraj.Route(start, []geom.Vec2{from, xy(from.X, 170)}, []float64{vel.Norm()})
			tr := conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Level(start, legs[0].T1, 36000)})
			want(k.ProposeRecontract(2, tr, kernel.Human), true, 0)
		case 515:
			k.Ack(2)
		case 700:
			k.Remove(3)
		}
		if math.Mod(now, 10) == 0 {
			k.Tick()
		}
		if math.Mod(now, 60) == 0 {
			k.ReanchorAll()
			checkpoint()
		}
	}
	if until >= 900 {
		if s := k.Stats; s.UnknownDropped != 1 || s.RecontractOK != 1 || s.ReanchorOK == 0 || k.Flight(2).Intruder {
			t.Fatalf("the script did not do what it is written to do: %+v", s)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func verify(t testing.TB, rec []byte, opt Options) *Report {
	t.Helper()
	rep, err := Verify(bytes.NewReader(rec), opt)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func classes(rep *Report) string {
	set := map[string]bool{}
	for _, f := range rep.Failures {
		set[string(f.Class)] = true
	}
	var out []string
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

var update = flag.Bool("update", false, "write the kernel vector of this package again")

// sessionVector is a published record of the session: a kernel vector
// (conformance/README.md). An arm64 machine wrote it.
const sessionVector = "../../conformance/kernel-session-0.2.rec"

// The kernel must do the same on every machine and in every version, unless
// its behaviour is changed on purpose. The session on this machine must give
// the bytes of the published vector: every input, every verdict with its
// conflict time, and every state digest. And this machine must replay the
// vector.
func TestKernelVectorSession(t *testing.T) {
	got := session(t, 900, false)
	if *update {
		if err := os.WriteFile(sessionVector, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(sessionVector)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		a, b := split(got), split(want)
		at := 0
		for at < len(a) && at < len(b) && bytes.Equal(a[at], b[at]) {
			at++
		}
		t.Fatalf("the session does not give the published vector: first difference at line %d.\n"+
			"If you changed what the kernel does, on purpose, write the vector again:\n"+
			"  go test ./internal/record -run TestKernelVectorSession -update\n"+
			"If you did not, the kernel computes other bits on this machine than on the one that wrote the vector.", at+1)
	}
	if rep := verify(t, want, Options{}); !rep.OK() || !rep.ReplayDone {
		t.Fatalf("this machine does not replay the vector: %v", rep.Failures)
	}
}

// A record of every kind of input passes every check.
func TestRecordPasses(t *testing.T) {
	rec := session(t, 900, false, keyA)
	rep := verify(t, rec, trust(keyA))
	if !rep.OK() || !rep.ReplayDone {
		t.Fatalf("the record does not pass: %v", rep.Failures)
	}
	for kd, name := range kindNames {
		if !bytes.Contains(rec, []byte(`{"k":"`+name+`"`)) {
			t.Errorf("the session has no input of kind %d (%s)", kd, name)
		}
	}
	for _, reason := range []string{"conflict", "duplicate", "lead", "authority"} {
		if !bytes.Contains(rec, []byte(`"reason":"`+reason+`"`)) {
			t.Errorf("the session has no rejection for reason %s", reason)
		}
	}
	if rep.Commits != 7 || rep.Rejects != 4 || rep.Checkpoints != 16 || rep.Tail != 0 || rep.Unsigned != 0 || rep.End != sec(900) {
		t.Fatalf("%d commits, %d rejects, %d checkpoints, tail %d, %d unsigned, end %v",
			rep.Commits, rep.Rejects, rep.Checkpoints, rep.Tail, rep.Unsigned, rep.End)
	}
	if rep.Replayed != rep.Lines || rep.Lines != rep.Inputs+rep.Commits+rep.Rejects+1 {
		t.Fatalf("%d lines, %d replayed, %d inputs", rep.Lines, rep.Replayed, rep.Inputs)
	}
	if len(rep.Signers) != 1 || !rep.Signers[0].Trusted || rep.Signers[0].Checkpoints != 16 {
		t.Fatalf("signers: %+v", rep.Signers)
	}
	// The same session gives the same bytes.
	if !bytes.Equal(rec, session(t, 900, false, keyA)) {
		t.Fatal("two runs of one session wrote different records")
	}
	t.Logf("%d lines, %d bytes", rep.Lines, len(rec))
}

func split(rec []byte) [][]byte {
	ls := bytes.Split(bytes.TrimSuffix(rec, []byte("\n")), []byte("\n"))
	out := make([][]byte, len(ls))
	for i, l := range ls {
		out[i] = append([]byte(nil), l...)
	}
	return out
}

func join(ls [][]byte) []byte { return append(bytes.Join(ls, []byte("\n")), '\n') }

// find returns the index of the n-th line (from 0) that contains s, at or
// after index from.
func find(t testing.TB, ls [][]byte, from int, s string, n int) int {
	t.Helper()
	for i := from; i < len(ls); i++ {
		if bytes.Contains(ls[i], []byte(s)) {
			if n == 0 {
				return i
			}
			n--
		}
	}
	t.Fatalf("no line with %s", s)
	return -1
}

func replace(t testing.TB, l []byte, old, new string) []byte {
	t.Helper()
	if bytes.Count(l, []byte(old)) != 1 {
		t.Fatalf("%q occurs %d times in %.120s", old, bytes.Count(l, []byte(old)), l)
	}
	return bytes.Replace(l, []byte(old), []byte(new), 1)
}

func remove(ls [][]byte, i int) [][]byte { return append(ls[:i:i], ls[i+1:]...) }

func insert(ls [][]byte, i int, l []byte) [][]byte {
	out := append(ls[:i:i], l)
	return append(out, ls[i:]...)
}

// flipHex changes one hexadecimal digit of the value of a name in a line.
func flipHex(t testing.TB, l []byte, name string) []byte {
	t.Helper()
	i := bytes.Index(l, []byte(`"`+name+`":"`))
	if i < 0 {
		t.Fatalf("no %s in %.120s", name, l)
	}
	i += len(name) + 4 + 5
	out := append([]byte(nil), l...)
	if out[i] == '0' {
		out[i] = '1'
	} else {
		out[i] = '0'
	}
	return out
}

// Each change to a record is found by the check that is there to find it.
// The cases also show what each check cannot find alone: the chain finds a
// change that leaves no trace in the kernel, and the replay finds a verdict
// that the chain and the signatures accept.
func TestEveryChangeIsFound(t *testing.T) {
	rec := session(t, 900, false, keyA)
	const cp = `"k":"checkpoint"`
	// In each case: the lines after the change, and the index of the line
	// that a chain failure must name, or -1.
	cases := []struct {
		name   string
		change func(ls [][]byte) ([][]byte, int)
		want   string // classes of the failures, sorted
	}{
		{"a track moves by one micrometre: no trace in the kernel", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"track","f":3`, 0)
			ls[i] = replace(t, ls[i], `"alt":11277600000`, `"alt":11277600001`)
			return ls, find(t, ls, i, cp, 0)
		}, "chain"},
		{"a track leaves its level: the flight becomes an intruder", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"track","f":3`, 0)
			ls[i] = replace(t, ls[i], `"alt":11277600000`, `"alt":11887200000`)
			return ls, find(t, ls, i, cp, 0)
		}, "chain replay"},
		{"an input line is removed", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"track","f":3`, 0)
			ls = remove(ls, i)
			return ls, find(t, ls, i, cp, 0)
		}, "chain"},
		{"an input line is repeated", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"tick"`, 0)
			ls = insert(ls, i, ls[i])
			return ls, find(t, ls, i, cp, 0)
		}, "chain"},
		{"two input lines change places", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"track","f":1`, 0)
			if !bytes.Contains(ls[i+1], []byte(`"k":"track","f":2`)) {
				t.Fatal("the next line is not a track of flight 2")
			}
			ls[i], ls[i+1] = ls[i+1], ls[i]
			return ls, find(t, ls, i, cp, 0)
		}, "chain"},
		{"an acknowledgement is removed", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, `"k":"ack","f":1`, 0)
			ls = remove(ls, i)
			return ls, find(t, ls, i, cp, 0)
		}, "chain replay"},
		{"a commit becomes a rejection", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, `"k":"propose"`, 0) + 1
			ls[i] = replace(t, ls[i], `"ok":true,"contract":4`, `"ok":false,"reason":"conflict","with":2,"at":300000000`)
			return ls, find(t, ls, i, cp, 0)
		}, "chain replay"},
		{"a rejection becomes a commit", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, `"reason":"lead"`, 0)
			ls[i] = []byte(`{"k":"verdict","ok":true,"contract":99}`)
			return ls, find(t, ls, i, cp, 0)
		}, "chain replay"},
		{"a parameter in the header changes", func(ls [][]byte) ([][]byte, int) {
			ls[0] = replace(t, ls[0], `"VTol":60960000`, `"VTol":30480000`)
			return ls, find(t, ls, 0, cp, 0)
		}, "chain replay"},
		{"the chain value of a checkpoint changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			ls[i] = flipHex(t, ls[i], "head")
			return ls, i
		}, "chain signature"},
		{"the line number of a checkpoint changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			c := checkpointOf(t, ls[i])
			ls[i] = replace(t, ls[i], fmt.Sprintf(`"seq":%d`, c.Seq), fmt.Sprintf(`"seq":%d`, c.Seq+1))
			return ls, i
		}, "chain signature"},
		{"the cell of a checkpoint changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			ls[i] = replace(t, ls[i], `"cell":"test-cell"`, `"cell":"other-cell"`)
			return ls, i
		}, "chain signature"},
		{"the state digest of a checkpoint changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			ls[i] = flipHex(t, ls[i], "state")
			return ls, -1
		}, "replay signature"},
		{"the time of a checkpoint changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			ls[i] = replace(t, ls[i], `"t":300000000`, `"t":300000001`)
			return ls, -1
		}, "replay signature"},
		{"a signature changes", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			ls[i] = flipHex(t, ls[i], "sig")
			return ls, -1
		}, "signature"},
		{"a checkpoint loses its signature", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			c := checkpointOf(t, ls[i])
			c.Sigs = nil
			ls[i] = c.MarshalLine()
			return ls, -1
		}, "signature"},
		{"another key signs a checkpoint", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, cp, 5)
			c := checkpointOf(t, ls[i])
			c.Sigs = nil
			c.Sign(keyB)
			ls[i] = c.MarshalLine()
			return ls, -1
		}, "signature"},
		{"a verdict line is removed", func(ls [][]byte) ([][]byte, int) {
			return remove(ls, find(t, ls, 0, `"k":"propose"`, 0)+1), -1
		}, "format"},
		{"a checkpoint moves between a proposal and its verdict", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 0, `"k":"propose"`, 0)
			c := find(t, ls, i, cp, 0)
			moved := ls[c]
			return insert(remove(ls, c), i+1, moved), -1
		}, "format"},
		{"a line is not JSON", func(ls [][]byte) ([][]byte, int) {
			return insert(ls, 50, []byte(`not a line`)), -1
		}, "format"},
		{"a line has a name that the format does not have", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"tick"`, 0)
			ls[i] = []byte(`{"k":"tick","comment":"x"}`)
			return ls, -1
		}, "format"},
		{"a line has a name of another kind", func(ls [][]byte) ([][]byte, int) {
			i := find(t, ls, 100, `"k":"tick"`, 0)
			ls[i] = []byte(`{"k":"tick","f":1}`)
			return ls, -1
		}, "format"},
		{"a second header", func(ls [][]byte) ([][]byte, int) {
			return insert(ls, 50, ls[0]), -1
		}, "format"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ls, chainAt := c.change(split(rec))
			rep := verify(t, join(ls), trust(keyA))
			if got := classes(rep); got != c.want {
				t.Fatalf("failures of class [%s], want [%s]:\n%v", got, c.want, rep.Failures)
			}
			chain := 0
			for _, f := range rep.Failures {
				if f.Class != Chain {
					continue
				}
				chain++
				if f.Line != chainAt+1 {
					t.Errorf("the chain failure names line %d; the first checkpoint after the change is line %d", f.Line, chainAt+1)
				}
			}
			if chain > 1 {
				t.Errorf("%d chain failures for one change: %v", chain, rep.Failures)
			}
			if rep.ReplayDone && strings.Contains(c.want, "replay") {
				t.Error("the replay is reported as done")
			}
		})
	}
}

func checkpointOf(t testing.TB, raw []byte) *Checkpoint {
	t.Helper()
	l, err := parseLine(raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err := l.checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// resign makes the checkpoints of a changed record fit its lines again, with
// new signatures: what a signer who rewrites history can do.
func resign(t testing.TB, ls [][]byte, key ed25519.PrivateKey) [][]byte {
	t.Helper()
	var head [32]byte
	for i, raw := range ls {
		if !bytes.Contains(raw, []byte(`"k":"checkpoint"`)) {
			head = link(head, raw)
			continue
		}
		c := checkpointOf(t, raw)
		c.Head, c.Sigs = head, nil
		c.Sign(key)
		ls[i] = c.MarshalLine()
	}
	return ls
}

// A checkpoint is a statement about the chain, not a part of it. A record
// without one of its checkpoints still passes: a later checkpoint covers the
// same lines. Lines after the last checkpoint are not covered, and the
// report says how many there are.
func TestCheckpointsAreNotChained(t *testing.T) {
	rec := session(t, 900, false, keyA)
	ls := split(rec)
	ls = remove(ls, find(t, ls, 0, `"k":"checkpoint"`, 5))
	rep := verify(t, join(ls), trust(keyA))
	if !rep.OK() || rep.Checkpoints != 15 || rep.Tail != 0 {
		t.Fatalf("one checkpoint less: %d checkpoints, tail %d, %v", rep.Checkpoints, rep.Tail, rep.Failures)
	}

	ls = split(rec)
	ls = ls[:len(ls)-1] // the last checkpoint
	ls = append(ls, []byte(`{"k":"advance","t":905000000}`))
	rep = verify(t, join(ls), trust(keyA))
	if !rep.OK() || rep.Tail == 0 || rep.End != sec(905) {
		t.Fatalf("a tail with no checkpoint: tail %d, end %v, %v", rep.Tail, rep.End, rep.Failures)
	}

	// A record that ends with a proposal: the kernel stopped before it
	// answered. The report says so.
	ls = split(session(t, 60, false, keyA))
	i := find(t, ls, 0, `"k":"propose"`, 0)
	rep = verify(t, join(ls[:i+1]), trust(keyA))
	if !rep.OK() || !rep.Unanswered {
		t.Fatalf("a proposal with no verdict at the end: %v, %v", rep.Unanswered, rep.Failures)
	}
}

// The signer of a record can rewrite it. A changed verdict with new
// signatures passes the chain and the signature checks, and the replay finds
// it. A different history that the kernel really produced passes every check
// of the record alone. Only a checkpoint that someone else holds shows that
// it is not the history that was first published.
func TestRewriteBySignerNeedsAWitness(t *testing.T) {
	first := session(t, 900, false, keyA)
	held, err := ReadCheckpoints(bytes.NewReader(first))
	if err != nil || len(held) != 16 {
		t.Fatalf("%d checkpoints, %v", len(held), err)
	}
	withWitness := trust(keyA)
	withWitness.Witnessed = held

	if rep := verify(t, first, withWitness); !rep.OK() || rep.Witnessed != 16 {
		t.Fatalf("the first record against its own checkpoints: %d found, %v", rep.Witnessed, rep.Failures)
	}

	// A changed verdict, signed again.
	ls := split(first)
	i := find(t, ls, 0, `"reason":"lead"`, 0)
	ls[i] = []byte(`{"k":"verdict","ok":false,"reason":"conflict","with":1,"at":400000000}`)
	changed := join(resign(t, ls, keyA))
	if rep := verify(t, changed, trust(keyA)); classes(rep) != "replay" {
		t.Fatalf("a changed verdict with new signatures: [%s] %v", classes(rep), rep.Failures)
	}
	noReplay := trust(keyA)
	noReplay.NoReplay = true
	if rep := verify(t, changed, noReplay); !rep.OK() {
		t.Fatalf("without the replay the changed verdict must pass: %v", rep.Failures)
	}

	// Another history from the same kernel and the same signer.
	other := session(t, 900, true, keyA)
	if rep := verify(t, other, trust(keyA)); !rep.OK() || !rep.ReplayDone {
		t.Fatalf("the other history alone: %v", rep.Failures)
	}
	rep := verify(t, other, withWitness)
	if classes(rep) != "witness" {
		t.Fatalf("the other history against the held checkpoints: [%s] %v", classes(rep), rep.Failures)
	}
	// The two histories are the same up to 400 s: checkpoints 0 to 6 are in
	// both, and the 9 later ones are not.
	if rep.Witnessed != 7 || len(rep.Failures) != 9 {
		t.Fatalf("%d held checkpoints found, %d failures", rep.Witnessed, len(rep.Failures))
	}

	// The first record without its end.
	ls = split(first)
	cut := find(t, ls, 0, `"k":"checkpoint"`, 9)
	short := join(ls[:cut+1])
	if rep := verify(t, short, trust(keyA)); !rep.OK() {
		t.Fatalf("a record that ends early passes alone: %v", rep.Failures)
	}
	rep = verify(t, short, withWitness)
	if classes(rep) != "witness" || rep.Witnessed != 10 || len(rep.Failures) != 6 {
		t.Fatalf("a record that ends early against the held checkpoints: %d found, %v", rep.Witnessed, rep.Failures)
	}

	// A held checkpoint that the trusted key did not sign proves nothing.
	forged := held[3]
	forged.Head[0] ^= 1
	forged.Sigs = nil
	forged.Sign(keyB)
	withWitness.Witnessed = []Checkpoint{forged}
	rep = verify(t, first, withWitness)
	if classes(rep) != "witness" || !strings.Contains(rep.Failures[0].Msg, "trusted signatures") {
		t.Fatalf("a held checkpoint from another key: %v", rep.Failures)
	}
}

// Without a trusted key, Verify shows that a record is consistent and no
// more. With trusted keys, each checkpoint needs a quorum of them.
func TestSigners(t *testing.T) {
	unsigned := session(t, 120, false)
	rep := verify(t, unsigned, Options{})
	if !rep.OK() || rep.Unsigned != 3 || len(rep.Signers) != 0 {
		t.Fatalf("an unsigned record with no trusted key: %d unsigned, %v", rep.Unsigned, rep.Failures)
	}
	if rep := verify(t, unsigned, trust(keyA)); classes(rep) != "signature" || len(rep.Failures) != 3 {
		t.Fatalf("an unsigned record against a trusted key: %v", rep.Failures)
	}

	byB := session(t, 120, false, keyB)
	rep = verify(t, byB, Options{})
	if !rep.OK() || len(rep.Signers) != 1 || rep.Signers[0].Trusted {
		t.Fatalf("a record by an unknown signer, no trusted key: %+v %v", rep.Signers, rep.Failures)
	}
	if rep := verify(t, byB, trust(keyA)); classes(rep) != "signature" {
		t.Fatalf("a record by another signer: %v", rep.Failures)
	}

	both := session(t, 120, false, keyA, keyB)
	two := trust(keyA, keyB)
	two.Quorum = 2
	rep = verify(t, both, two)
	if !rep.OK() || len(rep.Signers) != 2 {
		t.Fatalf("two signers, quorum two: %+v %v", rep.Signers, rep.Failures)
	}
	if rep := verify(t, session(t, 120, false, keyA), two); classes(rep) != "signature" {
		t.Fatalf("one signer, quorum two: %v", rep.Failures)
	}
	// One key that signs twice is one signer.
	if rep := verify(t, session(t, 120, false, keyA, keyA), two); classes(rep) != "signature" {
		t.Fatalf("one key twice, quorum two: %v", rep.Failures)
	}
}

// randInt draws an int64 of any size, with the limits among the values.
func randInt(r *rand.Rand) int64 {
	switch r.IntN(8) {
	case 0:
		return 0
	case 1:
		return -1
	case 2:
		return math.MaxInt64
	case 3:
		return math.MinInt64
	case 4:
		return int64(r.IntN(100000))
	}
	return int64(r.Uint64() >> r.IntN(64))
}

// A line holds an input exactly: every number comes back as it was written.
func TestInputsRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	n := func() int64 { return randInt(r) }
	trajectory := func() *traj.Trajectory {
		tr := &traj.Trajectory{}
		for i := r.IntN(4) + 1; i > 0; i-- {
			tr.Legs = append(tr.Legs, traj.Leg{T0: n(), T1: n(), P0: fx.Vec{X: n(), Y: n()}, V: fx.Vec{X: n(), Y: n()}})
			tr.VSegs = append(tr.VSegs, traj.VSeg{T0: n(), T1: n(), A0: n(), A1: n()})
		}
		return tr
	}
	for i := 0; i < 20000; i++ {
		in := kernel.Input{Kind: kernel.InputKind(r.IntN(int(kernel.NumInputKinds)))}
		id := kernel.FlightID(n())
		by := kernel.Authority(r.IntN(2))
		switch in.Kind {
		case kernel.InAdvance:
			in.T = n()
		case kernel.InAdmit:
			in.Flight, in.Traj = id, trajectory()
		case kernel.InPropose, kernel.InRecontract:
			in.Flight, in.Traj, in.By = id, trajectory(), by
		case kernel.InTrack:
			in.Flight = id
			in.Track = kernel.Track{T: n(), Pos: fx.Vec{X: n(), Y: n()}, Vel: fx.Vec{X: n(), Y: n()}, Alt: n(), VRate: n()}
			if r.IntN(2) == 0 {
				in.Track.HasSel, in.Track.SelAlt = true, n()
			}
		case kernel.InSetAuthority:
			in.Flight, in.By = id, by
		case kernel.InReanchor, kernel.InTick:
		default:
			in.Flight = id
		}
		var buf bytes.Buffer
		w, err := NewWriter(&buf, Header{Cell: "c", Verifier: "dual", Params: kernel.DefaultParams()})
		if err != nil {
			t.Fatal(err)
		}
		w.Input(in)
		if err := w.Close(); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		l, err := parseLine(split(buf.Bytes())[1])
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		back, err := l.input()
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if !reflect.DeepEqual(in, back) {
			t.Fatalf("an input changed:\n wrote %+v\n read  %+v\n line  %s", in, back, split(buf.Bytes())[1])
		}
	}
}

// A verdict line holds a verdict exactly.
func TestVerdictsRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(4, 6))
	for n := 0; n < 5000; n++ {
		v := verdict{OK: r.IntN(2) == 0}
		if v.OK {
			v.Contract = r.Uint64()
		} else {
			v.Reason = kernel.RejectReason(1 + r.IntN(int(kernel.NumReasons)-1))
			if v.Reason == kernel.RejConflict || v.Reason == kernel.RejDisagree {
				v.With, v.At = kernel.FlightID(r.IntN(1000)), randInt(r)
			}
		}
		raw := mustJSON(t, v.line())
		l, err := parseLine(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		back, err := l.verdict()
		if err != nil || v != back {
			t.Fatalf("a verdict changed: wrote %+v, read %+v (%v), line %s", v, back, err, raw)
		}
	}
}

// The Writer keeps the first error and writes nothing more. What it wrote
// before is a record that passes.
func TestWriterKeepsFirstError(t *testing.T) {
	bad := []struct {
		name string
		do   func(w *Writer)
	}{
		{"a verdict with no proposal", func(w *Writer) { w.Verdict(kernel.Verdict{}) }},
		{"an input before the verdict of a proposal", func(w *Writer) {
			w.Input(kernel.Input{Kind: kernel.InAdmit, Flight: 1})
			w.Input(kernel.Input{Kind: kernel.InTick})
		}},
		{"a checkpoint before the verdict of a proposal", func(w *Writer) {
			w.Input(kernel.Input{Kind: kernel.InAdmit, Flight: 1})
			w.Checkpoint(0, [32]byte{})
		}},
		{"an input of no kind", func(w *Writer) { w.Input(kernel.Input{Kind: kernel.NumInputKinds}) }},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			w, err := NewWriter(&buf, Header{Cell: "c", Verifier: "dual", Params: kernel.DefaultParams()}, keyA)
			if err != nil {
				t.Fatal(err)
			}
			w.Input(kernel.Input{Kind: kernel.InAdvance, T: 1})
			c.do(w)
			first := w.Err()
			if first == nil {
				t.Fatal("no error")
			}
			w.Input(kernel.Input{Kind: kernel.InAdvance, T: 2})
			w.Checkpoint(2, [32]byte{})
			if err := w.Close(); err != first {
				t.Fatalf("Close returned %v, the first error was %v", err, first)
			}
			if bytes.Contains(buf.Bytes(), []byte(`"t":2`)) || bytes.Contains(buf.Bytes(), []byte("checkpoint")) {
				t.Fatalf("the Writer wrote after the error:\n%s", buf.Bytes())
			}
			opt := trust(keyA)
			opt.NoReplay = true
			if rep := verify(t, buf.Bytes(), opt); !rep.OK() {
				t.Fatalf("the record before the error does not pass: %v", rep.Failures)
			}
		})
	}

	for _, h := range []Header{
		{Cell: "", Verifier: "dual", Params: kernel.DefaultParams()},
		{Cell: "two words", Verifier: "dual", Params: kernel.DefaultParams()},
		{Cell: "c", Verifier: "", Params: kernel.DefaultParams()},
		{Cell: "c", Verifier: "dual"},
	} {
		if _, err := NewWriter(&bytes.Buffer{}, h); err == nil {
			t.Errorf("NewWriter accepted the header %+v", h)
		}
	}
}

func mustJSON(t testing.TB, l *line) []byte {
	t.Helper()
	b, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Each line that is not of the format is refused, with the reason. A number
// that is not a whole number is not of the format.
func TestLinesAreChecked(t *testing.T) {
	good := `{"k":"track","f":1,"t":5000000,"p":[1,-2],"v":[246933333,0],"alt":10668000000,"vr":0}`
	if _, err := parseLine([]byte(good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		``:                     "not a line",
		`null`:                 "unknown kind",
		`[]`:                   "not a line",
		`{"k":"track"}`:        `must have "f"`,
		`{"k":"launch","f":1}`: "unknown kind",
		good + `{"k":"tick"}`:  "text after",
		good + ` x`:            "text after",
		`{"k":"track","f":1.5,"t":5,"p":[1,2],"v":[0,0],"alt":0,"vr":0}`:                 "not a line",
		`{"k":"track","f":1,"t":5.5,"p":[1,2],"v":[0,0],"alt":0,"vr":0}`:                 "not a line",
		`{"k":"track","f":1,"t":5,"p":[1.25,2],"v":[0,0],"alt":0,"vr":0}`:                "not a line",
		`{"k":"track","f":1,"t":5,"p":[1,2],"v":[0,0],"alt":1e3,"vr":0}`:                 "not a line",
		`{"k":"track","f":1,"t":5,"p":[1,2,3],"v":[0,0],"alt":0,"vr":0}`:                 "two numbers",
		`{"k":"track","f":1,"t":5,"p":[1],"v":[0,0],"alt":0,"vr":0}`:                     "two numbers",
		`{"k":"track","f":1,"t":9223372036854775808,"p":[1,2],"v":[0,0],"alt":0,"vr":0}`: "not a line",
		`{"k":"track","f":1,"t":"5","p":[1,2],"v":[0,0],"alt":0,"vr":0}`:                 "not a line",
		`{"k":"advance"}`:             `must have "t"`,
		`{"k":"advance","t":1,"f":1}`: `must not have "f"`,
		`{"k":"admit","f":1}`:         `must have "traj"`,
		`{"k":"admit","f":1,"traj":{"legs":[{"t0":0.5,"t1":1,"p0":[0,0],"v":[0,0]}],"vsegs":[]}}`: "not a line",
		`{"k":"propose","f":1,"traj":{"legs":[],"vsegs":[]}}`:                                     `must have "by"`,
		`{"k":"verdict"}`:           `must have "ok"`,
		`{"k":"verdict","ok":true}`: `must have "contract"`,
		`{"k":"verdict","ok":true,"contract":1,"reason":"lead"}`:  `must not have "reason"`,
		`{"k":"verdict","ok":false}`:                              `must have "reason"`,
		`{"k":"verdict","ok":false,"reason":"conflict"}`:          `must have "with"`,
		`{"k":"verdict","ok":false,"reason":"lead","with":1}`:     `must not have "with"`,
		`{"k":"checkpoint","cell":"c","t":0,"seq":0,"head":"00"}`: `must have "state"`,
	}
	for raw, want := range bad {
		_, err := parseLine([]byte(raw))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n  error %v, want one with %q", raw, err, want)
		}
	}
	// Values that the names allow and the format does not.
	for raw, want := range map[string]string{
		`{"k":"authority","f":1,"by":"pilot"}`:                                 "unknown authority",
		`{"k":"verdict","ok":false,"reason":"mood"}`:                           "unknown reason",
		`{"k":"checkpoint","cell":"c","t":0,"seq":0,"head":"00","state":"00"}`: "64 hexadecimal digits",
	} {
		l, err := parseLine([]byte(raw))
		if err != nil {
			t.Errorf("%s: %v", raw, err)
			continue
		}
		switch l.K {
		case kVerdict:
			_, err = l.verdict()
		case kCheckpoint:
			_, err = l.checkpoint()
		default:
			_, err = l.input()
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n  error %v, want one with %q", raw, err, want)
		}
	}
}

// Keys have a text form, and a private key is never read as a public key.
func TestKeyText(t *testing.T) {
	p, err := ParsePublicKey(FormatPublicKey(pub(keyA)) + "\n")
	if err != nil || !p.Equal(pub(keyA)) {
		t.Fatalf("public key: %v", err)
	}
	k, err := ParsePrivateKey(FormatPrivateKey(keyA) + "\n")
	if err != nil || !k.Equal(keyA) {
		t.Fatalf("private key: %v", err)
	}
	if _, err := ParsePublicKey(FormatPrivateKey(keyA)); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Fatalf("a private key was read as a public key: %v", err)
	}
	if _, err := ParsePrivateKey(FormatPublicKey(pub(keyA))); err == nil {
		t.Fatal("a public key was read as a private key")
	}
	if _, err := ParsePublicKey("abc"); err == nil {
		t.Fatal("a short public key was read")
	}
}

// A record is untrusted data. These records are well formed, and their
// inputs are at or beyond the limits of valid values. Verify must finish and
// report on each one. The kernel must not stop on any of them, and it must
// refuse what is outside the limits.
func TestHostileInputs(t *testing.T) {
	const big = math.MaxInt64
	one := func(t0, t1 int64, p, v fx.Vec, a0, a1 int64) *traj.Trajectory {
		return &traj.Trajectory{Legs: []traj.Leg{{T0: t0, T1: t1, P0: p, V: v}}, VSegs: []traj.VSeg{{T0: t0, T1: t1, A0: a0, A1: a1}}}
	}
	level := ft(35000)
	still := kernel.Track{T: sec(10), Alt: level}
	scripts := map[string][]kernel.Input{
		"a clock beyond its last valid time": {
			{Kind: kernel.InAdmit, Flight: 1, Traj: straight(0, geom.Vec2{X: -160}, geom.Vec2{X: 160}, 35000)},
			{Kind: kernel.InAdvance, T: big}, {Kind: kernel.InAdvance, T: fx.MaxTime}, {Kind: kernel.InAdvance, T: fx.MaxTime / 2},
			{Kind: kernel.InAdmit, Flight: 2, Traj: one(fx.MaxTime/2, fx.MaxTime, fx.Vec{}, fx.Vec{X: 1}, 0, 0)},
			{Kind: kernel.InTick}, {Kind: kernel.InReanchor},
		},
		"an aircraft that does not move": {
			{Kind: kernel.InAdmit, Flight: 1, Traj: one(0, sec(5000), fx.Vec{X: 200 * fx.NM}, fx.Vec{}, level, level)},
			{Kind: kernel.InAdmit, Flight: 2, Traj: straight(0, geom.Vec2{X: -160}, geom.Vec2{X: 260}, 35000)},
			{Kind: kernel.InAdvance, T: sec(10)},
			{Kind: kernel.InTrack, Flight: 1, Track: still}, {Kind: kernel.InTrack, Flight: 2, Track: still},
			{Kind: kernel.InMarkIntruder, Flight: 1}, {Kind: kernel.InMarkIntruder, Flight: 2},
			{Kind: kernel.InTrack, Flight: 1, Track: still}, {Kind: kernel.InTick}, {Kind: kernel.InReanchor},
			{Kind: kernel.InRecontract, Flight: 1, Traj: one(sec(200), sec(5000), fx.Vec{}, fx.Vec{}, level, level)},
			{Kind: kernel.InAdmit, Flight: 3, Traj: straight(10, geom.Vec2{Y: -160}, geom.Vec2{Y: 160}, 35000)},
		},
		"the largest numbers": {
			{Kind: kernel.InAdmit, Flight: 1, Traj: one(0, big, fx.Vec{X: big, Y: -big}, fx.Vec{X: big, Y: big}, -big, big)},
			{Kind: kernel.InAdmit, Flight: 2, Traj: one(math.MinInt64, big, fx.Vec{X: math.MinInt64}, fx.Vec{X: math.MinInt64}, math.MinInt64, big)},
			{Kind: kernel.InAdmit, Flight: 3, Traj: one(-fx.MaxTime, fx.MaxTime, fx.Vec{X: -fx.MaxCoord, Y: fx.MaxCoord}, fx.Vec{X: 1}, fx.MaxCoord, -fx.MaxCoord)},
			{Kind: kernel.InAdmit, Flight: 4, Traj: straight(0, geom.Vec2{X: -160}, geom.Vec2{X: 160}, 35000)},
			{Kind: kernel.InAdvance, T: sec(5)},
			{Kind: kernel.InTrack, Flight: 4, Track: kernel.Track{T: big, Pos: fx.Vec{X: big}, Vel: fx.Vec{X: -big, Y: big}, Alt: big, VRate: -big, HasSel: true, SelAlt: big}},
			{Kind: kernel.InTrack, Flight: 4, Track: kernel.Track{T: sec(5), Pos: fx.Vec{X: fx.MaxTrack, Y: -fx.MaxTrack}, Vel: fx.Vec{X: fx.MaxSpeed, Y: -fx.MaxSpeed}, Alt: -fx.MaxTrack, VRate: fx.MaxSpeed, HasSel: true, SelAlt: fx.MaxTrack}},
			{Kind: kernel.InTick}, {Kind: kernel.InReanchor},
			{Kind: kernel.InPropose, Flight: 4, Traj: one(sec(200), sec(400), fx.Vec{X: big}, fx.Vec{X: big}, big, big)},
			{Kind: kernel.InRecontract, Flight: 4, By: kernel.Automation, Traj: one(big-1, big, fx.Vec{X: fx.MaxCoord}, fx.Vec{X: fx.MaxSpeed}, 0, 0)},
			{Kind: kernel.InRecontract, Flight: 4, By: kernel.Automation, Traj: one(fx.MaxTime-1, fx.MaxTime, fx.Vec{X: fx.MaxCoord}, fx.Vec{X: 0}, 0, 0)},
			{Kind: kernel.InTick},
		},
		"the slowest aircraft and the shortest leg": {
			{Kind: kernel.InAdmit, Flight: 1, Traj: one(0, 1, fx.Vec{X: 200 * fx.NM}, fx.Vec{X: 1}, 1, 0)},
			{Kind: kernel.InAdmit, Flight: 2, Traj: one(0, sec(5000), fx.Vec{X: 200 * fx.NM}, fx.Vec{X: 1, Y: 1}, level, level)},
			{Kind: kernel.InAdvance, T: 1}, {Kind: kernel.InTick},
			{Kind: kernel.InTrack, Flight: 2, Track: kernel.Track{T: 1, Pos: fx.Vec{X: 200*fx.NM + fx.NM/2}, Vel: fx.Vec{X: 1}, Alt: level}},
			{Kind: kernel.InReanchor}, {Kind: kernel.InMarkIntruder, Flight: 2}, {Kind: kernel.InTick},
		},
		"inputs for flights that do not exist, and empty trajectories": {
			{Kind: kernel.InAck, Flight: 9}, {Kind: kernel.InRefuse, Flight: 9}, {Kind: kernel.InFail, Flight: 9},
			{Kind: kernel.InRemove, Flight: 9}, {Kind: kernel.InMarkIntruder, Flight: 9}, {Kind: kernel.InSetAuthority, Flight: 9},
			{Kind: kernel.InTrack, Flight: 9, Track: still},
			{Kind: kernel.InAdmit, Flight: 1}, {Kind: kernel.InPropose, Flight: 1}, {Kind: kernel.InRecontract, Flight: 1},
			{Kind: kernel.InAdmit, Flight: 1, Traj: &traj.Trajectory{Legs: []traj.Leg{{T0: 0, T1: 1}}}},
			{Kind: kernel.InAdvance, T: -1}, {Kind: kernel.InTick}, {Kind: kernel.InReanchor},
		},
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			p := kernel.DefaultParams()
			w, err := NewWriter(&buf, Header{Cell: "hostile", Verifier: "dual", Params: p})
			if err != nil {
				t.Fatal(err)
			}
			// Write the inputs with a verdict that no kernel gave: the
			// replay will differ, and it must still apply every input.
			for _, in := range script {
				w.Input(in)
				if in.Kind.HasVerdict() {
					w.Verdict(kernel.Verdict{Reason: kernel.RejMalformed})
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			rep := verify(t, buf.Bytes(), Options{})
			for _, f := range rep.Failures {
				if f.Class != Replay || strings.Contains(f.Msg, "the kernel stopped") {
					t.Errorf("%v", f)
				}
			}
			// The same inputs straight into a kernel, to see its verdicts.
			d, _ := kernel.Arrangement("dual")
			k := kernel.New(p, d)
			var got []string
			for _, in := range script {
				if v, has := k.Apply(in); has {
					got = append(got, verdictOf(v).String())
				}
			}
			k.StateDigest()
			if k.Now() > fx.MaxTime/2 {
				t.Errorf("the clock is at %d, beyond its last valid time", k.Now())
			}
			t.Log(strings.Join(got, "; "))
		})
	}
}

// Verify must report on any bytes at all. It must not stop, and the kernel
// must not stop on an input that a line can hold.
func FuzzVerify(f *testing.F) {
	rec := session(f, 60, false, keyA)
	f.Add(rec)
	ls := split(rec)
	f.Add(join(ls[:8]))
	for i := 6; i+12 <= len(ls); i += 12 {
		f.Add(join(append(append([][]byte{}, ls[0]), ls[i:i+12]...)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		rep, err := Verify(bytes.NewReader(data), Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, fl := range rep.Failures {
			if strings.Contains(fl.Msg, "the kernel stopped") {
				t.Fatalf("%v", fl)
			}
		}
	})
}

// nativeProgram runs the kernel in Rust as a native program, if it has been
// built: `cargo build --release --offline -p clearway-kernel-host` in guest/.
// It is a development aid. The program that counts runs in the machine of
// package rvm; internal/kimage tests that one.
type nativeProgram string

func (p nativeProgram) Run(input []byte) ([]byte, uint32, uint64, error) {
	cmd := exec.Command(string(p))
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		return out, uint32(exit.ExitCode()), 0, nil
	}
	return out, 0, 0, err
}

// The kernel in Rust, as a native program, reproduces the record of the Go
// kernel: every verdict and every state digest of the session.
func TestRustKernelNative(t *testing.T) {
	const bin = "../../guest/target/release/kernel-host"
	if _, err := os.Stat(bin); err != nil {
		t.Skip("the native kernel program is not built")
	}
	for _, until := range []float64{60, 900} {
		rec := session(t, until, false)
		rep, err := ReplayProgram(bytes.NewReader(rec), nativeProgram(bin))
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() || rep.Verdicts == 0 || rep.Digests == 0 {
			t.Fatalf("session to %v s: %d verdicts, %d digests, failures: %v", until, rep.Verdicts, rep.Digests, rep.Failures)
		}
		t.Logf("session to %v s: %d verdicts and %d state digests reproduced, %d bytes of input", until, rep.Verdicts, rep.Digests, rep.Input)
	}
	// The published traffic vector, and any records named in CLEARWAY_RECORDS
	// (a list of files separated by colons).
	files := []string{"../../conformance/kernel-traffic-0.2.rec.gz"}
	if more := os.Getenv("CLEARWAY_RECORDS"); more != "" {
		files = append(files, strings.Split(more, ":")...)
	}
	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		var r io.Reader = f
		if strings.HasSuffix(name, ".gz") {
			if r, err = gzip.NewReader(f); err != nil {
				t.Fatal(err)
			}
		}
		rep, err := ReplayProgram(r, nativeProgram(bin))
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() {
			t.Fatalf("%s: after %d verdicts and %d digests: %v", name, rep.Verdicts, rep.Digests, rep.Failures)
		}
		t.Logf("%s: %d verdicts and %d state digests reproduced, %d bytes of input", filepath.Base(name), rep.Verdicts, rep.Digests, rep.Input)
	}
}
