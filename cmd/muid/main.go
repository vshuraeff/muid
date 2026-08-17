package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vshuraeff/muid"
)

func main() {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	n := flags.Int("n", 1, "number of IDs to generate")
	d := flags.String("d", "", "decode an ID")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if flags.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected argument %q\n", flags.Arg(0))
		os.Exit(1)
	}

	var nSet, dSet bool
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "n":
			nSet = true
		case "d":
			dSet = true
		}
	})

	if nSet && dSet {
		fmt.Fprintln(os.Stderr, "error: -n and -d cannot be used together")
		os.Exit(1)
	}

	if dSet {
		parsed, err := muid.Parse(*d)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		raw, err := parsed.MarshalBinary()
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if len(raw) < 4 {
			fmt.Fprintln(os.Stderr, "error: parsed ID has fewer than 4 tail bytes")
			os.Exit(1)
		}

		timestamp := parsed.Time()
		fmt.Fprintf(os.Stdout, "id: %s\n", parsed.String())
		fmt.Fprintf(os.Stdout, "time: %s (unix_ns: %d)\n", timestamp.UTC().Format(time.RFC3339Nano), timestamp.UnixNano())
		fmt.Fprintf(os.Stdout, "tail: %s\n", hex.EncodeToString(raw[len(raw)-4:]))
		return
	}

	if *n < 1 {
		fmt.Fprintln(os.Stderr, "error: -n must be at least 1")
		os.Exit(1)
	}

	for i := 0; i < *n; i++ {
		fmt.Fprintln(os.Stdout, muid.NewString())
	}
}
