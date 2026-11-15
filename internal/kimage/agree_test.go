package kimage

import (
	"bytes"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"testing"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/record"
	"github.com/teddyknox/clearway/internal/traj"
)

// The two kernels must agree on every input, and simulated traffic has only
// usual inputs. This file builds sessions of unusual ones: aircraft that do
// not move or that move a micrometre in a second, a radius that grows faster
// than the aircraft flies, legs of one microsecond, several vertical
// segments, values at the limits of what is valid, and proposals that the
// kernel must refuse. The Go kernel applies each session and writes its
// record. The kernel in Rust must then reproduce every verdict and every
// state digest.

type unusual struct {
	r    *rand.Rand
	k    *kernel.Kernel
	w    *record.Writer
	next kernel.FlightID
	slow int // contracts committed with several vertical segments and a lowest speed at or below K
}

func floorM(x int64) int64 {
	q := x / fx.M
	if x%fx.M < 0 {
		q--
	}
	return q
}

// velocity returns a ground velocity (µm/s): none, almost none, slow, usual,
// or as fast as a valid one can be.
func (g *unusual) velocity() fx.Vec {
	var m int64
	switch g.r.IntN(9) {
	case 0:
		return fx.Vec{}
	case 1:
		m = 1 + g.r.Int64N(1000)
	case 2, 3:
		m = 1 + g.r.Int64N(2*g.k.P.K+fx.Kt)
	case 4:
		m = fx.MaxSpeed
	default:
		m = 200*fx.Kt + g.r.Int64N(300*fx.Kt)
	}
	a := g.r.Float64() * 2 * math.Pi
	return fx.Vec{X: int64(float64(m) * math.Cos(a)), Y: int64(float64(m) * math.Sin(a))}
}

var levels = []int64{30000 * fx.FT, 31000 * fx.FT, 32000 * fx.FT, 35000 * fx.FT, 35500 * fx.FT, 36000 * fx.FT, 39000 * fx.FT}

// trajectory builds a trajectory from t0 at p0 (µm) and alt. Its legs join to
// the micrometre. Most end outside the cell: the last leg flies straight out.
func (g *unusual) trajectory(t0 int64, p0 fx.Vec, alt int64) *traj.Trajectory {
	tr := &traj.Trajectory{}
	t, p := t0, p0
	for n := 1 + g.r.IntN(4); n > 0; n-- {
		dur := fx.Sec + g.r.Int64N(1500*fx.Sec)
		if g.r.IntN(12) == 0 {
			dur = 1 + g.r.Int64N(fx.Sec)
		}
		l := traj.Leg{T0: t, T1: t + dur, P0: p, V: g.velocity()}
		tr.Legs = append(tr.Legs, l)
		end := l.Pos(l.T1)
		t, p = l.T1, fx.Vec{X: floorM(end.X), Y: floorM(end.Y)}
	}
	if g.r.IntN(5) != 0 {
		// Straight out, at 450 kt, to twice the radius of the cell.
		d := math.Hypot(float64(p.X), float64(p.Y))
		ux, uy := 1.0, 0.0
		if d > 0 {
			ux, uy = float64(p.X)/d, float64(p.Y)/d
		}
		v := float64(450 * fx.Kt)
		dur := int64(math.Max(0, 2*float64(g.k.P.CellRadius)-d)/v*float64(fx.Sec)) + 60*fx.Sec
		tr.Legs = append(tr.Legs, traj.Leg{T0: t, T1: t + dur, P0: p, V: fx.Vec{X: int64(v * ux), Y: int64(v * uy)}})
		t += dur
	}
	// Vertical segments: cut the time at random, and hold or change the level.
	nv := 1 + g.r.IntN(5)
	cuts := []int64{t0}
	for i := 1; i < nv && t-t0 > int64(nv); i++ {
		cuts = append(cuts, t0+1+g.r.Int64N(t-t0-1))
	}
	cuts = append(cuts, t)
	for i := range cuts {
		for j := i + 1; j < len(cuts); j++ {
			if cuts[j] < cuts[i] {
				cuts[i], cuts[j] = cuts[j], cuts[i]
			}
		}
	}
	for i := 0; i+1 < len(cuts); i++ {
		if cuts[i+1] == cuts[i] {
			continue
		}
		to := alt
		if g.r.IntN(2) == 0 {
			to = levels[g.r.IntN(len(levels))]
		}
		tr.VSegs = append(tr.VSegs, traj.VSeg{T0: cuts[i], T1: cuts[i+1], A0: alt, A1: to})
		alt = to
	}
	if g.r.IntN(25) == 0 {
		tr.Legs[0].P0.X += fx.MaxCoord // not valid
	}
	return tr
}

func (g *unusual) apply(in kernel.Input) kernel.Verdict {
	v, _ := g.k.Apply(in)
	return v
}

// committed counts a contract with several vertical segments whose lowest
// speed is at or below the growth of its radius: the case in which a segment
// that the aircraft has left can come back into effect.
func (g *unusual) committed(c *kernel.Contract) {
	if c != nil && len(c.Traj.VSegs) > 1 && c.VMin > 0 && c.VMin <= c.K {
		g.slow++
	}
}

func (g *unusual) flight() *kernel.Flight {
	ids := g.k.IDs()
	if len(ids) == 0 {
		return nil
	}
	return g.k.Flight(ids[g.r.IntN(len(ids))])
}

// track returns a track for f at the current time: on its contract, off it,
// or not valid at all.
func (g *unusual) track(f *kernel.Flight, on bool) kernel.Track {
	now := g.k.Now()
	tk := kernel.Track{T: now, Alt: levels[g.r.IntN(len(levels))]}
	switch {
	case f.Active != nil && now >= f.Active.Start() && now <= f.Active.End():
		at := f.Active.Traj.Pos(now)
		tk.Pos = fx.Vec{X: floorM(at.X), Y: floorM(at.Y)}
		tk.Vel = f.Active.Traj.LegAt(now).V
		tk.Alt = f.Active.Traj.AltAt(now)
	case f.HasTrack:
		tk.Pos, tk.Vel = f.Track.Pos, f.Track.Vel
	}
	spread := int64(fx.NM / 5)
	if !on {
		spread = 6 * fx.NM
		tk.Vel = g.velocity()
		tk.Alt += g.r.Int64N(4000*fx.FT) - 2000*fx.FT
		tk.VRate = g.r.Int64N(60*fx.FT) - 30*fx.FT
	}
	tk.Pos.X += g.r.Int64N(2*spread+1) - spread
	tk.Pos.Y += g.r.Int64N(2*spread+1) - spread
	if g.r.IntN(3) == 0 {
		tk.HasSel, tk.SelAlt = true, tk.Alt
		if g.r.IntN(3) == 0 {
			tk.SelAlt = levels[g.r.IntN(len(levels))]
		}
	}
	if !on && g.r.IntN(15) == 0 {
		tk.Pos.X, tk.Vel.Y = math.MaxInt64, math.MinInt64 // not valid
	}
	return tk
}

func (g *unusual) step() {
	now, p := g.k.Now(), g.k.P
	f := g.flight()
	by := kernel.Automation
	if f != nil {
		by = f.Authority
		if g.r.IntN(20) == 0 {
			by = 1 - by
		}
	}
	switch n := g.r.IntN(100); {
	case n < 26 || f == nil:
		g.next++
		id := g.next
		if g.r.IntN(15) == 0 && f != nil {
			id = f.ID // a duplicate
		}
		t0 := now - 200*fx.Sec + g.r.Int64N(500*fx.Sec)
		p0 := fx.Vec{X: g.r.Int64N(400*fx.NM) - 200*fx.NM, Y: g.r.Int64N(400*fx.NM) - 200*fx.NM}
		if v := g.apply(kernel.Input{Kind: kernel.InAdmit, Flight: id, Traj: g.trajectory(t0, p0, levels[g.r.IntN(len(levels))])}); v.OK {
			g.committed(g.k.Flight(id).Active)
		}
	case n < 50:
		g.apply(kernel.Input{Kind: kernel.InTrack, Flight: f.ID, Track: g.track(f, g.r.IntN(4) != 0)})
	case n < 60:
		step := []int64{1, fx.Sec, 5 * fx.Sec, 30 * fx.Sec, 60 * fx.Sec, 600 * fx.Sec}[g.r.IntN(6)]
		g.apply(kernel.Input{Kind: kernel.InAdvance, T: now + step})
	case n < 68:
		g.apply(kernel.Input{Kind: kernel.InTick})
	case n < 74:
		// A re-anchor needs tracks taken at the current time.
		for _, id := range g.k.IDs() {
			if g.r.IntN(3) != 0 {
				g.apply(kernel.Input{Kind: kernel.InTrack, Flight: id, Track: g.track(g.k.Flight(id), true)})
			}
		}
		g.apply(kernel.Input{Kind: kernel.InReanchor})
	case n < 84:
		// An amendment: a tail from a point of the active contract.
		td := now + p.MinLead + g.r.Int64N(300*fx.Sec)
		at, alt := fx.Vec{}, levels[0]
		if f.Active != nil {
			pm := f.Active.Traj.Pos(td)
			at = fx.Vec{X: floorM(pm.X), Y: floorM(pm.Y)}
			alt, _ = f.Active.Band(td, 0)
		}
		switch g.r.IntN(12) {
		case 0:
			at.X += fx.NM
		case 1:
			alt += 100 * fx.FT
		case 2:
			td = now + g.r.Int64N(p.MinLead)
		}
		if v := g.apply(kernel.Input{Kind: kernel.InPropose, Flight: f.ID, By: by, Traj: g.trajectory(td, at, alt)}); v.OK {
			g.committed(f.Pending)
		}
	case n < 89:
		// A new contract for an intruder, from where its track will be.
		ts := now + p.MinLead + g.r.Int64N(120*fx.Sec)
		tk := f.Track
		at := fx.Vec{X: tk.Pos.X + floorM(tk.Vel.X*((ts-tk.T)/1000)*1000), Y: tk.Pos.Y + floorM(tk.Vel.Y*((ts-tk.T)/1000)*1000)}
		if !f.HasTrack || tk.T > now || ts-tk.T > 4000*fx.Sec {
			at = fx.Vec{}
		}
		if v := g.apply(kernel.Input{Kind: kernel.InRecontract, Flight: f.ID, By: by, Traj: g.trajectory(ts, at, tk.Alt)}); v.OK {
			g.committed(f.Pending)
		}
	case n < 94:
		g.apply(kernel.Input{Kind: kernel.InAck, Flight: f.ID})
	case n < 96:
		g.apply(kernel.Input{Kind: []kernel.InputKind{kernel.InRefuse, kernel.InFail}[g.r.IntN(2)], Flight: f.ID})
	case n < 97:
		g.apply(kernel.Input{Kind: kernel.InRemove, Flight: f.ID})
	case n < 98:
		g.apply(kernel.Input{Kind: kernel.InMarkIntruder, Flight: f.ID})
	case n < 99:
		g.apply(kernel.Input{Kind: kernel.InSetAuthority, Flight: f.ID, By: kernel.Authority(g.r.IntN(2))})
	default:
		g.apply(kernel.Input{Kind: kernel.InAck, Flight: 1 << 40}) // no such flight
	}
}

// unusualSession returns the record of one session, and what the Go kernel
// did in it.
func unusualSession(t testing.TB, seed uint64) (rec []byte, g *unusual, d *kernel.Dual) {
	r := rand.New(rand.NewPCG(seed, 77))
	p := kernel.DefaultParams()
	switch r.IntN(5) {
	case 0:
		p.K = 0
	case 1:
		p.K = 40 * fx.Kt
	case 2:
		p.IntruderK, p.IntentCheck = 20*fx.Kt, false
	}
	arrangement := []string{"dual", "dual", "single"}[r.IntN(3)]
	d, err := kernel.Arrangement(arrangement)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w, err := record.NewWriter(&buf, record.Header{Cell: "unusual", Verifier: arrangement, Params: p})
	if err != nil {
		t.Fatal(err)
	}
	g = &unusual{r: r, k: kernel.New(p, d), w: w}
	g.k.Rec = w
	for i, n := 0, 60+r.IntN(80); i < n; i++ {
		g.step()
		if i%7 == 6 {
			w.Checkpoint(g.k.Now(), g.k.StateDigest())
		}
	}
	w.Checkpoint(g.k.Now(), g.k.StateDigest())
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), g, d
}

// native runs the kernel in Rust as a native program.
type native string

func (p native) Run(input []byte) ([]byte, uint32, uint64, error) {
	cmd := exec.Command(string(p))
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if exit, ok := err.(*exec.ExitError); ok {
		return out, uint32(exit.ExitCode()), 0, nil
	}
	return out, 0, 0, err
}

func TestKernelsAgreeOnUnusualInputs(t *testing.T) {
	run := func(t *testing.T, prog record.Program, first, n uint64) {
		t.Helper()
		var st kernel.Stats
		var verdicts, digests, slow int
		var marginal, steps uint64
		for seed := first; seed < first+n; seed++ {
			rec, g, d := unusualSession(t, seed)
			rep, err := record.ReplayProgram(bytes.NewReader(rec), prog)
			if err != nil {
				t.Fatalf("session %d: %v", seed, err)
			}
			if !rep.OK() {
				t.Fatalf("session %d: the two kernels differ after %d verdicts and %d state digests: %v", seed, rep.Verdicts, rep.Digests, rep.Failures)
			}
			verdicts, digests, steps = verdicts+rep.Verdicts, digests+rep.Digests, steps+rep.Steps
			slow, marginal = slow+g.slow, marginal+d.Marginal
			s := g.k.Stats
			st.Commits += s.Commits
			st.ReanchorOK += s.ReanchorOK
			st.ReanchorRejected += s.ReanchorRejected
			st.RecontractOK += s.RecontractOK
			st.Alarms += s.Alarms
			for i := range s.Rejects {
				st.Rejects[i] += s.Rejects[i]
			}
			for i := range s.Exits {
				st.Exits[i] += s.Exits[i]
			}
		}
		t.Logf("%d sessions: %d verdicts and %d state digests reproduced, in %d instructions", n, verdicts, digests, steps)
		t.Logf("commits %d, rejects by reason %v, re-anchors %d accepted and %d rejected, exits %v, re-contracts %d, marginal disagreements %d, slow contracts with several segments %d, alarms %d",
			st.Commits, st.Rejects, st.ReanchorOK, st.ReanchorRejected, st.Exits, st.RecontractOK, marginal, slow, st.Alarms)
		// The sessions must reach the kernel's less usual paths. These are
		// floors for each session on average, far below what the sessions give.
		if st.Commits < 10*n || st.Rejects[kernel.RejConflict] < n/2 || st.ReanchorOK < 5*n || uint64(slow) < 2*n || uint64(digests) < 8*n {
			t.Errorf("weak sessions: %d commits, %d conflicts, %d re-anchors, %d slow contracts in %d sessions", st.Commits, st.Rejects[kernel.RejConflict], st.ReanchorOK, slow, n)
		}
	}
	t.Run("in the machine", func(t *testing.T) {
		n := uint64(24)
		if testing.Short() {
			n = 6
		}
		run(t, Machine{}, 1, n)
	})
	t.Run("as a native program", func(t *testing.T) {
		const bin = "../../guest/target/release/kernel-host"
		if _, err := os.Stat(bin); err != nil {
			t.Skip("the native kernel program is not built")
		}
		run(t, native(bin), 1000, 600)
	})
}
