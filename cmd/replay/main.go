// Command replay runs the event stream of the brief through the ledger and
// prints, for each of Days 1 to 6, the closing ledger balance, the fee
// assessments, the authorization states and the errors.
//
//	go run ./cmd/replay
package main

import (
	"fmt"
	"os"

	"ledgercore/internal/replay"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
}

func run() error {
	result, err := replay.Run(replay.Stream())
	if err != nil {
		return err
	}
	return replay.Print(os.Stdout, result)
}
