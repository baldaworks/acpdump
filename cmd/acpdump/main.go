package main

import (
	"os"

	"github.com/baldaworks/acpdump/cmd/acpdump/cmd"
)

func main() {
	if err := command.Command().Execute(); err != nil {
		os.Exit(1)
	}
}
