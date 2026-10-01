// validate runs only detection on explicitly supplied local images.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sahithyandev/nemo/internal/validation"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(errOut)
	manifest := flags.String("manifest", "", "version 1 expected-findings JSON manifest")
	flags.Usage = func() { fmt.Fprintln(errOut, "Usage: validate -manifest FILE DATASET_ROOT"); flags.PrintDefaults() }
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *manifest == "" || flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	f, err := os.Open(*manifest)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	m, err := validation.Parse(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	report, err := validation.Run(flags.Arg(0), m)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	if !report.Complete() {
		return 1
	}
	return 0
}
