package rvm

import (
	"bytes"
	"io"
	"testing"
)

// benchGuest runs the guest program on an input b.N times and reports the
// instructions that Run executes in one second. The time of Load is not in
// the measurement.
func benchGuest(b *testing.B, input []byte, prepare func(*Machine)) {
	elf := loadGuest(b)
	var steps uint64
	for b.Loop() {
		b.StopTimer()
		m, err := Load(elf, guestMem)
		if err != nil {
			b.Fatal(err)
		}
		m.In, m.Out = bytes.NewReader(input), io.Discard
		if prepare != nil {
			prepare(m)
		}
		b.StartTimer()
		if _, err := m.Run(guestStepLimit); err != nil {
			b.Fatal(err)
		}
		steps += m.Steps
	}
	// The only floating-point value of the package: the testing package
	// takes a metric as a float64.
	if ns := uint64(b.Elapsed().Nanoseconds()); ns > 0 {
		b.ReportMetric(float64(steps*1000/ns), "Minsn/s")
	}
	b.ReportMetric(float64(steps/uint64(b.N)), "insn/op")
}

// BenchmarkRun measures the speed of Run on the guest program.
func BenchmarkRun(b *testing.B) {
	sha := newGuestRun(1)
	for range 16 {
		sha.sha256(1 << 16)
	}
	wide := newGuestRun(1)
	for range 20 {
		wide.arith128(500)
	}
	copies := newGuestRun(1)
	for range 20 {
		copies.mem(4000)
	}
	workload := newGuestWorkload(1)

	b.Run("workload", func(b *testing.B) { benchGuest(b, workload.in, nil) })
	b.Run("sha256", func(b *testing.B) { benchGuest(b, sha.in, nil) })
	b.Run("arith128", func(b *testing.B) { benchGuest(b, wide.in, nil) })
	b.Run("memcpy", func(b *testing.B) { benchGuest(b, copies.in, nil) })
	b.Run("workload-nocache", func(b *testing.B) {
		benchGuest(b, workload.in, func(m *Machine) { m.noCache = true })
	})
}

// BenchmarkStep measures the cost of one call of Step.
func BenchmarkStep(b *testing.B) {
	m := newTest(code(b, loadGolden(b), asmSpin))
	for b.Loop() {
		if err := m.Step(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStateDigest measures StateDigest on the memory of the guest.
func BenchmarkStateDigest(b *testing.B) {
	m, err := Load(loadGuest(b), guestMem)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(guestMem)
	for b.Loop() {
		m.StateDigest()
	}
}
