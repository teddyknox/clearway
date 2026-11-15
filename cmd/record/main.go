// Command record checks the record of a kernel. docs/record.md defines the
// format and says what each check shows.
//
//	record verify run.rec                      check the chain and replay the kernel
//	record verify -key cell.pub run.rec        also check who signed the checkpoints
//	record verify -witness held.ckpt run.rec   also check the record against checkpoints held elsewhere
//	record verify -vm run.rec                  also replay the record with the kernel program, in the RISC-V machine
//	record checkpoints run.rec                 print the checkpoints of a record: what a witness keeps
//	record keygen -out cell                    write a new signing key to cell.key and cell.pub
//
// A file whose name ends in .gz is read as gzip. atcsim -record writes a
// record.
package main

import (
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/teddyknox/clearway/internal/kimage"
	"github.com/teddyknox/clearway/internal/record"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage:
  record verify [-key KEY]... [-quorum N] [-witness FILE] [-no-replay] [-vm] RECORD
  record checkpoints RECORD
  record keygen -out NAME
`

// run executes one command. It returns the exit status: 0 if the command
// succeeded and the record passed, 1 if the record did not pass or the
// command failed, 2 if the arguments are wrong.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "verify":
		var pass bool
		if pass, err = verify(args[1:], stdout, stderr); err == nil && !pass {
			return 1
		}
	case "checkpoints":
		err = checkpoints(args[1:], stdout)
	case "keygen":
		err = keygen(args[1:], stdout, stderr)
	default:
		err = errUsage
	}
	switch {
	case errors.Is(err, errUsage):
		fmt.Fprint(stderr, usage)
		return 2
	case err != nil:
		fmt.Fprintln(stderr, "record:", err)
		return 1
	}
	return 0
}

var errUsage = errors.New("usage")

// keyList collects each -key argument: a public key, or a file that holds
// one.
type keyList []ed25519.PublicKey

func (k *keyList) String() string { return fmt.Sprint(len(*k), " keys") }

func (k *keyList) Set(s string) error {
	text := s
	if b, err := os.ReadFile(s); err == nil {
		text = string(b)
	}
	key, err := record.ParsePublicKey(text)
	if err != nil {
		return fmt.Errorf("%q is not a public key, and not a file that holds one: %v", s, err)
	}
	*k = append(*k, key)
	return nil
}

func verify(args []string, stdout, stderr io.Writer) (pass bool, err error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var keys keyList
	fs.Var(&keys, "key", "a public key that you trust, or a file that holds one; repeat for more")
	quorum := fs.Int("quorum", 1, "trusted keys that must sign each checkpoint")
	witness := fs.String("witness", "", "a file of checkpoints that you got from a source other than the record")
	noReplay := fs.Bool("no-replay", false, "do not replay the kernel")
	vm := fs.Bool("vm", false, "also replay the record with the kernel program, in the RISC-V machine (slow)")
	if fs.Parse(args) != nil || fs.NArg() != 1 {
		return false, errUsage
	}
	opt := record.Options{Trusted: keys, Quorum: *quorum, NoReplay: *noReplay}
	if *witness != "" {
		f, err := open(*witness)
		if err != nil {
			return false, err
		}
		opt.Witnessed, err = record.ReadCheckpoints(f)
		f.Close()
		if err != nil {
			return false, fmt.Errorf("%s: %v", *witness, err)
		}
		if len(opt.Witnessed) == 0 {
			return false, fmt.Errorf("%s holds no checkpoint", *witness)
		}
	}
	f, err := open(fs.Arg(0))
	if err != nil {
		return false, err
	}
	defer f.Close()
	rep, err := record.Verify(f, opt)
	if err != nil {
		return false, err
	}
	print(stdout, fs.Arg(0), rep, opt)
	pass = rep.OK()
	if *vm {
		ok, err := program(stdout, fs.Arg(0))
		if err != nil {
			return false, err
		}
		pass = pass && ok
	}
	result(stdout, pass)
	return pass, nil
}

// program replays the record with the kernel program in the machine, and
// prints what it found.
func program(w io.Writer, path string) (bool, error) {
	f, err := open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	rep, err := record.ReplayProgram(f, kimage.Machine{})
	if err != nil {
		return false, err
	}
	p := func(label, format string, args ...any) {
		fmt.Fprintf(w, "%-10s %s\n", label, fmt.Sprintf(format, args...))
	}
	if rep.OK() {
		p("program", "the kernel program %s, in the RISC-V machine, reproduced", kimage.SHA256[:16])
		p("", "%d verdicts and %d state digests, in %d instructions", rep.Verdicts, rep.Digests, rep.Steps)
	} else {
		p("program", "the kernel program %s, in the RISC-V machine, does not reproduce the record", kimage.SHA256[:16])
		p("", "after %d verdicts and %d state digests", rep.Verdicts, rep.Digests)
	}
	for _, f := range rep.Failures {
		p("FAIL", "%s", f)
	}
	return rep.OK(), nil
}

func result(w io.Writer, pass bool) {
	if pass {
		fmt.Fprintf(w, "%-10s %s\n", "result", "PASS")
	} else {
		fmt.Fprintf(w, "%-10s %s\n", "result", "FAIL")
	}
}

// print writes the result of one verification. For each check it says what
// was checked, or that the check did not run.
func print(w io.Writer, path string, rep *record.Report, opt record.Options) {
	p := func(label, format string, args ...any) {
		fmt.Fprintf(w, "%-10s %s\n", label, fmt.Sprintf(format, args...))
	}
	h := rep.Header
	p("record", "%s", path)
	if h.Cell != "" {
		p("", "format %s, specification %s, cell %s, verifier %s", h.Format, h.Spec, h.Cell, h.Verifier)
		if h.Note != "" {
			p("", "note: %s", h.Note)
		}
		p("lines", "%d chained: %d inputs, %d commits, %d rejects; kernel time %d.%06d s",
			rep.Lines, rep.Inputs, rep.Commits, rep.Rejects, rep.End/1_000_000, rep.End%1_000_000)
		p("chain", "%d checkpoints; chain value %s after the last line", rep.Checkpoints, hex.EncodeToString(rep.Head[:8]))
		if rep.Tail > 0 {
			p("", "%d lines after the last checkpoint: no checkpoint covers them", rep.Tail)
		}
		if rep.Unanswered {
			p("", "the record ends with a proposal that has no verdict")
		}

		for _, s := range rep.Signers {
			trust := "a key that the record names"
			if s.Trusted {
				trust = "a key that you trust"
			}
			p("signed", "%d checkpoints by %s, %s", s.Checkpoints, s.Key, trust)
		}
		if rep.Unsigned > 0 {
			p("signed", "%d checkpoints have no signature", rep.Unsigned)
		}
		if len(opt.Trusted) == 0 {
			p("", "not checked against a key that you trust: give -key. Anyone can make a record that passes without it")
		}

		if len(opt.Witnessed) > 0 {
			p("witness", "the record contains %d of %d witnessed checkpoints", rep.Witnessed, len(opt.Witnessed))
		} else {
			p("witness", "not checked: give -witness. The signer can rewrite a record that no witness holds")
		}

		switch {
		case opt.NoReplay:
			p("replay", "not run")
		case rep.ReplayDone:
			p("replay", "the kernel of this program reproduced every verdict and every state digest (%s %s/%s)",
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
		default:
			p("replay", "the kernel of this program does not reproduce the record (%s %s/%s)",
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
			if rep.StateDiffs > 0 || rep.ReasonDiffs > 0 {
				p("", "checkpoints with another state: %d; proposals rejected for another reason: %d", rep.StateDiffs, rep.ReasonDiffs)
			}
			p("", "a replay needs the version of the kernel that wrote the record (docs/record.md, section 8): check that first")
		}
	}
	for _, f := range rep.Failures {
		p("FAIL", "%s", f)
	}
}

func checkpoints(args []string, stdout io.Writer) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errUsage
	}
	f, err := open(args[0])
	if err != nil {
		return err
	}
	defer f.Close()
	cps, err := record.ReadCheckpoints(f)
	if err != nil {
		return err
	}
	for i := range cps {
		if _, err := fmt.Fprintf(stdout, "%s\n", cps[i].MarshalLine()); err != nil {
			return err
		}
	}
	return nil
}

// open opens a record. It reads a file whose name ends in .gz as gzip.
func open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil || !strings.HasSuffix(path, ".gz") {
		return f, err
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return struct {
		io.Reader
		io.Closer
	}{z, f}, nil
}

func keygen(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write NAME.key and NAME.pub")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *out == "" {
		return errUsage
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	// O_EXCL: never replace a key that exists.
	if err := writeNew(*out+".key", record.FormatPrivateKey(priv)+"\n", 0o600); err != nil {
		return err
	}
	if err := writeNew(*out+".pub", record.FormatPublicKey(pub)+"\n", 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s.key: the private key. Keep it out of version control.\n", *out)
	fmt.Fprintf(stdout, "wrote %s.pub: the public key, %s\n", *out, record.FormatPublicKey(pub))
	return nil
}

func writeNew(path, text string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
