package main

import (
	"io"

	"github.com/douhashi/hikidashi/internal/extract"
)

// runExtract は hikidashi extract の入口。Stop の async hook から起動され、hook と同じく常に exit 0 で終える。
func runExtract(_ []string, stdin io.Reader, _, stderr io.Writer) int {
	return runFromHook("extract", stderr, func(dataRoot string) error {
		return extract.Run(dataRoot, stdin)
	})
}
