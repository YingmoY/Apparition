package main

import (
	"os"

	"github.com/YingmoY/Apparition/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
