package kimage

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teddyknox/clearway/internal/record"
)

func TestImageHasItsDigest(t *testing.T) {
	if got := Digest(ELF); got != SHA256 {
		t.Fatalf("kernel.elf has the digest %s; the package states %s. Run guest/build.sh and update SHA256.", got, SHA256)
	}
}

func open(t *testing.T, name string) io.Reader {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".gz") {
		return bytes.NewReader(data)
	}
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return z
}

// The kernel program, in the machine, reproduces the published kernel
// vectors: every verdict and every state digest that the Go kernel wrote, bit
// for bit. This is the test that two kernels in two languages, one of them on
// a fixed instruction set, compute the same thing.
func TestProgramReproducesTheVectors(t *testing.T) {
	for _, c := range []struct {
		name              string
		verdicts, digests int
		long              bool
	}{
		{"../../conformance/kernel-session-0.2.rec", 11, 16, false},
		{"../../conformance/kernel-traffic-0.2.rec.gz", 428, 30, true},
	} {
		if c.long && testing.Short() {
			continue
		}
		start := time.Now()
		rep, err := record.ReplayProgram(open(t, c.name), Machine{})
		if err != nil {
			t.Fatal(err)
		}
		if !rep.OK() || rep.Verdicts != c.verdicts || rep.Digests != c.digests {
			t.Fatalf("%s: %d verdicts, %d state digests, failures: %v", c.name, rep.Verdicts, rep.Digests, rep.Failures)
		}
		t.Logf("%s: %d verdicts and %d state digests reproduced in %d instructions (%.1f s)",
			c.name, rep.Verdicts, rep.Digests, rep.Steps, time.Since(start).Seconds())
	}
}
