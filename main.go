package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	options, err := parseOptions(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := runWebMap(options); err != nil {
		fmt.Fprintln(os.Stderr, "codemap:", err)
		os.Exit(1)
	}
}

func parseOptions(arguments []string) (mapOptions, error) {
	flags := flag.NewFlagSet("codemap", flag.ContinueOnError)
	baseRevision := flags.String("base", "", "Git revision to compare against, such as origin/main")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage of %s [options]\n", os.Args[0])
		fmt.Fprintf(flags.Output(), "\nOptions:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return mapOptions{}, err
	}
	if flags.NArg() != 0 {
		return mapOptions{}, fmt.Errorf("unknown argument %q", strings.Join(flags.Args(), " "))
	}
	return mapOptions{BaseRevision: strings.TrimSpace(*baseRevision)}, nil
}
