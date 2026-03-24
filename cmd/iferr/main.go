package main

import (
	iferr "github.com/imjasonh/iferr-analyzer"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(iferr.Analyzer)
}
