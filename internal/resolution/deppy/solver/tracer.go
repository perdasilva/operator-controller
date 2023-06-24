package solver

import (
	"fmt"
	"github.com/perdasilva/replee/pkg/deppy"
	"io"
)

type DefaultTracer struct{}

func (DefaultTracer) Trace(_ deppy.SearchPosition) {
}

type LoggingTracer struct {
	Writer io.Writer
}

func (t LoggingTracer) Trace(p deppy.SearchPosition) {
	fmt.Fprintf(t.Writer, "---\nAssumptions:\n")
	for _, i := range p.Variables() {
		fmt.Fprintf(t.Writer, "- %s\n", i.Identifier())
	}
	fmt.Fprintf(t.Writer, "Conflicts:\n")
	for _, a := range p.Conflicts() {
		fmt.Fprintf(t.Writer, "- %s\n", a)
	}
}
