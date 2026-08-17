package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vshuraeff/muid"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("muid", flag.ContinueOnError)
	flags.SetOutput(stderr)

	n := flags.Int("n", 1, "number of µIDs to generate")
	d := flags.String("d", "", "decode a µID")

	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	if flags.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument %q\n", flags.Arg(0))
		return 1
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
		fmt.Fprintln(stderr, "error: -n and -d cannot be used together")
		return 1
	}

	if dSet {
		parsed, err := muid.Parse(*d)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}

		raw, err := parsed.MarshalBinary()
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}

		timestamp := parsed.Time()
		fmt.Fprintf(stdout, "id: %s\n", parsed.String())
		fmt.Fprintf(stdout, "time: %s (unix_ns: %d)\n", timestamp.UTC().Format(time.RFC3339Nano), binary.BigEndian.Uint64(raw[:8]))
		fmt.Fprintf(stdout, "rand: %s\n", hex.EncodeToString(raw[8:10]))
		fmt.Fprintf(stdout, "crc: %s\n", hex.EncodeToString(raw[10:12]))
		return 0
	}

	if *n < 1 {
		fmt.Fprintln(stderr, "error: -n must be at least 1")
		return 1
	}

	for i := 0; i < *n; i++ {
		fmt.Fprintln(stdout, muid.NewString())
	}

	return 0
}
