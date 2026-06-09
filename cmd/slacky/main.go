package main

import (
	"os"

	"github.com/andyhtran/slacky/internal/app"
)

var version = "dev"

func main() {
	os.Exit(app.Run(version))
}
