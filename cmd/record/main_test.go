package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teddyknox/clearway/internal/record"
	"github.com/teddyknox/clearway/internal/sim"
)

func command(args ...string) (stdout, stderr string, status int) {
	var out, errOut bytes.Buffer
	status = run(args, &out, &errOut)
	return out.String(), errOut.String(), status
}

// The command from end to end: make a key, check a record that the simulator
// wrote, give the checkpoints to a witness, and find a change.
func TestCommand(t *testing.T) {
	dir := t.TempDir()
	path := func(name string) string { return filepath.Join(dir, name) }

	// keygen writes a private key that only the owner can read, and never
	// replaces a key.
	out, errOut, status := command("keygen", "-out", path("cell"))
	if status != 0 {
		t.Fatalf("keygen: status %d, %s", status, errOut)
	}
	keyText, err := os.ReadFile(path("cell.key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := record.ParsePrivateKey(string(keyText))
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(path("cell.key")); info.Mode().Perm() != 0o600 {
		t.Fatalf("the private key has mode %v", info.Mode().Perm())
	}
	if strings.Contains(out, strings.TrimSpace(strings.TrimPrefix(string(keyText), "clearway-private-key:"))) {
		t.Fatal("keygen printed the private key")
	}
	if _, _, status := command("keygen", "-out", path("cell")); status != 1 {
		t.Fatalf("keygen over a key that exists: status %d", status)
	}
	if again, _ := os.ReadFile(path("cell.key")); !bytes.Equal(again, keyText) {
		t.Fatal("keygen replaced a key")
	}

	// A record from the simulator.
	cfg := sim.DefaultConfig()
	cfg.Duration = 600
	var rec bytes.Buffer
	if _, err := sim.RunRecord(cfg, &rec, key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path("run.rec"), rec.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, status = command("verify", "-key", path("cell.pub"), path("run.rec"))
	if status != 0 {
		t.Fatalf("verify: status %d\n%s%s", status, out, errOut)
	}
	for _, want := range []string{"result     PASS", "a key that you trust", "reproduced every verdict", "witness    not checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify with a key: no %q in\n%s", want, out)
		}
	}
	// Without a key the record still passes, and the output says what that
	// does not show.
	out, _, status = command("verify", path("run.rec"))
	if status != 0 || !strings.Contains(out, "not checked against a key that you trust") {
		t.Fatalf("verify with no key: status %d\n%s", status, out)
	}

	// The checkpoints, as a witness keeps them.
	held, _, status := command("checkpoints", path("run.rec"))
	if status != 0 || strings.Count(held, "\n") != 10 {
		t.Fatalf("checkpoints: status %d, %d lines", status, strings.Count(held, "\n"))
	}
	if err := os.WriteFile(path("held.ckpt"), []byte(held), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, status = command("verify", "-key", path("cell.pub"), "-witness", path("held.ckpt"), path("run.rec"))
	if status != 0 || !strings.Contains(out, "the record contains 10 of 10 witnessed checkpoints") {
		t.Fatalf("verify with a witness: status %d\n%s", status, out)
	}

	// One changed digit.
	changed := bytes.Replace(rec.Bytes(), []byte(`{"k":"advance","t":300000000}`), []byte(`{"k":"advance","t":300000001}`), 1)
	if bytes.Equal(changed, rec.Bytes()) {
		t.Fatal("the record has no line to change")
	}
	if err := os.WriteFile(path("changed.rec"), changed, 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, status = command("verify", "-key", path("cell.pub"), path("changed.rec"))
	if status != 1 || !strings.Contains(out, "FAIL       chain") || !strings.Contains(out, "result     FAIL") {
		t.Fatalf("verify of a changed record: status %d\n%s", status, out)
	}

	// Another key.
	if _, errOut, status := command("keygen", "-out", path("other")); status != 0 {
		t.Fatal(errOut)
	}
	out, _, status = command("verify", "-key", path("other.pub"), path("run.rec"))
	if status != 1 || !strings.Contains(out, "FAIL       signature") {
		t.Fatalf("verify against another key: status %d\n%s", status, out)
	}
	// A private key is refused where a public key belongs.
	if _, errOut, status := command("verify", "-key", path("cell.key"), path("run.rec")); status != 2 || !strings.Contains(errOut, "private key") {
		t.Fatalf("verify with a private key as -key: status %d, %s", status, errOut)
	}

	for _, args := range [][]string{{}, {"verify"}, {"verify", "a", "b"}, {"checkpoints"}, {"keygen"}, {"launch"}} {
		if _, _, status := command(args...); status != 2 {
			t.Errorf("record %v: status %d, want 2", args, status)
		}
	}
	if _, _, status := command("verify", path("absent.rec")); status != 1 {
		t.Errorf("verify of a file that does not exist: status %d", status)
	}
}

// With -vm the command also replays the record with the kernel program, in
// the machine. The program must reproduce a published record, and it must
// report a record that states another verdict.
func TestCommandWithTheKernelProgram(t *testing.T) {
	const vector = "../../conformance/kernel-session-0.2.rec"
	out, errOut, status := command("verify", "-vm", vector)
	if status != 0 {
		t.Fatalf("verify -vm: status %d\n%s%s", status, out, errOut)
	}
	for _, want := range []string{"in the RISC-V machine, reproduced", "11 verdicts and 16 state digests", "as translated code", "result     PASS"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify -vm: no %q in\n%s", want, out)
		}
	}
	// The interpreter gives the same line, with the same count of
	// instructions.
	slow, _, status := command("verify", "-vm", "-interpret", vector)
	if status != 0 || strings.ReplaceAll(slow, "interpreted", "as translated code") != out {
		t.Errorf("verify -vm -interpret: status %d\n%s\nwithout -interpret:\n%s", status, slow, out)
	}
	if _, _, status := command("verify", "-interpret", vector); status != 2 {
		t.Errorf("verify -interpret without -vm: status %d, want 2", status)
	}

	rec, err := os.ReadFile(vector)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Replace(rec, []byte(`"reason":"duplicate"`), []byte(`"reason":"busy"`), 1)
	if bytes.Equal(changed, rec) {
		t.Fatal("the vector has no verdict to change")
	}
	path := filepath.Join(t.TempDir(), "changed.rec")
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, status = command("verify", "-vm", path)
	if status != 1 {
		t.Fatalf("verify -vm of a changed record: status %d\n%s", status, out)
	}
	for _, want := range []string{"does not reproduce the record", `the kernel program says "rejected: duplicate"`, "result     FAIL"} {
		if !strings.Contains(out, want) {
			t.Errorf("verify -vm of a changed record: no %q in\n%s", want, out)
		}
	}
}
