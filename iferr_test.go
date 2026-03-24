package iferr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	iferr "github.com/imjasonh/iferr-analyzer"
)

func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.RunWithSuggestedFixes(t, testdata, iferr.Analyzer, "a")
}
