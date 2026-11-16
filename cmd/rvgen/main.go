// Command rvgen translates a program for the machine of internal/rvm into
// Go source: host code that reaches the same states as the interpreter,
// faster. See rvm.Translate and docs/vm.md.
//
//	rvgen -pkg kimage -name kernelCode -o internal/kimage/kcode.go internal/kimage/kernel.elf
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/teddyknox/clearway/internal/rvm"
)

func main() {
	pkg := flag.String("pkg", "", "the package of the file to write")
	name := flag.String("name", "", "the name of the *rvm.Code variable to declare")
	mem := flag.Uint("mem", 64<<20, "the memory of the machine that will run the program (bytes)")
	out := flag.String("o", "", "the file to write")
	flag.Parse()
	if flag.NArg() != 1 || *pkg == "" || *name == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: rvgen -pkg PACKAGE -name NAME -o FILE.go PROGRAM.elf")
		os.Exit(2)
	}
	image, err := os.ReadFile(flag.Arg(0))
	if err == nil {
		var src []byte
		if src, err = rvm.Translate(image, uint32(*mem), *pkg, *name); err == nil {
			err = os.WriteFile(*out, src, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "rvgen:", err)
		os.Exit(1)
	}
}
