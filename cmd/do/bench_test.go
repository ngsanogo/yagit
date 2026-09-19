package main

import "testing"

// The outputs below are go's own, copied from verbose runs of this
// repository's benchmark rather than written from its documentation: the two
// lines that look most alike — the bare name a verbose run prints before it
// times anything, and the result that follows — are the two this parser has
// to tell apart.

func TestReadBenchOutput(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   benchReport
	}{
		{
			name: "a skipped benchmark is a skip and not a result",
			// The run this file exists for: exit 0, PASS, and nothing timed.
			output: `goos: linux
goarch: arm64
pkg: github.com/ngsanogo/yagit/internal/graph
BenchmarkCrossingAPage
    real_test.go:74: set YAGIT_REAL_REPOSITORY to a repository to measure Crossing against it
--- SKIP: BenchmarkCrossingAPage
PASS
ok  	github.com/ngsanogo/yagit/internal/graph	0.011s
`,
			want: benchReport{measured: 0, skipped: 1},
		},
		{
			name: "each sub-benchmark is a result, and the bare names are not",
			output: `pkg: github.com/ngsanogo/yagit/internal/graph
BenchmarkCrossingAPage
    real_test.go:103: 85719 commits from /tmp/git, 107406 lines in the picture
BenchmarkCrossingAPage/index
BenchmarkCrossingAPage/index-12         	  596791	      2276 ns/op	       201.0 edges/page	   12264 B/op	       9 allocs/op
BenchmarkCrossingAPage/scan
BenchmarkCrossingAPage/scan-12          	   22725	     52648 ns/op	       201.0 edges/page	   12240 B/op	       8 allocs/op
PASS
ok  	github.com/ngsanogo/yagit/internal/graph	3.153s
`,
			want: benchReport{measured: 2, skipped: 0},
		},
		{
			name: "a skip beside a result counts both",
			output: `BenchmarkAssign
BenchmarkAssign-12      	    1000	   1052 ns/op
BenchmarkCrossingAPage
--- SKIP: BenchmarkCrossingAPage
`,
			want: benchReport{measured: 1, skipped: 1},
		},
		{
			name: "a skipped sub-benchmark is indented under its parent",
			output: `BenchmarkCrossingAPage
BenchmarkCrossingAPage/scan
    --- SKIP: BenchmarkCrossingAPage/scan
`,
			want: benchReport{measured: 0, skipped: 1},
		},
		{
			name: "a log line that starts with the word is not a result",
			// Four fields and the right first word; what it lacks is the
			// iteration count.
			output: "    Benchmark data: 85719 commits read\nBenchmarks are slow here\n",
			want:   benchReport{},
		},
		{
			name:   "packages with no benchmark say nothing to count",
			output: "PASS\nok  \tgithub.com/ngsanogo/yagit/internal/git\t0.005s\n",
			want:   benchReport{},
		},
		{name: "no output at all", output: "", want: benchReport{}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := readBenchOutput(testCase.output); got != testCase.want {
				t.Errorf("readBenchOutput = %+v, want %+v", got, testCase.want)
			}
		})
	}
}
