package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// testBench times what the other tests only check the answer of.
//
// Not a gate and not part of `./do test`, for the same reason fuzzing is not:
// a number that depends on the machine it was measured on cannot fail a pull
// request. It is the tool for the question asked before an optimisation and
// again after it, which is the only way to know one was worth committing.
//
// Without -race, unlike every other Go test here. The detector intercepts
// every memory access, and a benchmark run under it measures the detector.
//
// -run '^$' matches no test at all: a benchmark run that also ran the suite
// would spend most of its time somewhere the numbers do not report.
//
// -v is what makes a skip visible at all. Without it go prints nothing for a
// benchmark that called b.Skip — not its name, not its reason — and exits 0,
// so a run that timed nothing reads exactly like a package with no benchmark
// in it. The reason stays where it is written, in the benchmark; what is
// counted here is only whether anything was timed.
//
// A run that timed nothing is an error, for the reason `./do test soak 0` is
// refused: PASS under every package is true, green, and answers nothing.
func (p *project) testBench(pattern string) error {
	info("benchmarks matching %s", pattern)

	args := append([]string{"test", "-run", "^$", "-bench", pattern, "-benchmem", "-v"}, goPackages...)
	command, err := p.tool("go", args...)
	if err != nil {
		return err
	}

	// Written to the terminal as it arrives and kept as well: a benchmark
	// takes seconds per line, and holding the output back until the end to
	// read it would look like a hang.
	var output bytes.Buffer
	command.Stdout = io.MultiWriter(os.Stdout, &output)
	if err := command.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}

	report := readBenchOutput(output.String())
	switch {
	case report.measured == 0 && report.skipped == 0:
		return fmt.Errorf("nothing was measured: no benchmark matches %q in %s",
			pattern, strings.Join(goPackages, " "))
	case report.measured == 0:
		return fmt.Errorf("nothing was measured: %d skipped, and no other benchmark matches %q.%s",
			report.skipped, pattern, indent(
				"Each one says why above, on the line under its name."))
	case report.skipped > 0:
		warn("%d skipped beside the %d results above; each says why on the line under its name",
			report.skipped, report.measured)
	}
	return nil
}

// benchReport is what a verbose `go test -bench` run says it did.
type benchReport struct {
	// measured counts result lines, so a benchmark with two sub-benchmarks
	// is two.
	measured int
	skipped  int
}

// readBenchOutput counts the results and the skips in go's benchmark output.
//
// A result is a line in the format go documents for its benchmarks: a name
// starting with Benchmark, an iteration count, then values and their units.
// The iteration count is what tells it from the bare name a verbose run
// prints before it starts timing, and from a benchmark's own log lines.
func readBenchOutput(output string) benchReport {
	var report benchReport
	for line := range strings.Lines(output) {
		// A sub-benchmark's skip is indented under its parent.
		if strings.HasPrefix(strings.TrimSpace(line), "--- SKIP: Benchmark") {
			report.skipped++
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		if iterations, err := strconv.Atoi(fields[1]); err == nil && iterations > 0 {
			report.measured++
		}
	}
	return report
}
