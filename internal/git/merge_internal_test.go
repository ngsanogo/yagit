package git

import "testing"

// `git rev-list --left-right --count` answers with two counts and nothing
// else. parseLeftRightCount is the one place that reading is turned into the
// numbers a merge or rebase preview reasons about, and it errors rather than
// degrading, because a preview that guessed would offer the user the wrong
// operation.
func TestParseLeftRightCount(t *testing.T) {
	cases := []struct {
		output string
		left   int
		right  int
		fails  bool
	}{
		{output: "0\t0\n", left: 0, right: 0},
		{output: "3\t7\n", left: 3, right: 7},
		{output: "  12   4  ", left: 12, right: 4},

		{output: "", fails: true},
		{output: "5\n", fails: true},
		{output: "1\t2\t3\n", fails: true},
		{output: "one\ttwo\n", fails: true},

		// A sign is what `--count` cannot produce, and the reason it must not
		// pass is downstream: outcomeOf tests `behind == 0` and then
		// `ahead == 0`, so a negative matches neither and names an outcome by
		// falling past both.
		{output: "-1\t0\n", fails: true},
		{output: "0\t-1\n", fails: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.output, func(t *testing.T) {
			left, right, err := parseLeftRightCount(testCase.output)

			if testCase.fails {
				if err == nil {
					t.Fatalf("%q parsed as %d/%d, expected an error", testCase.output, left, right)
				}
				// Nothing usable comes back with the error: a caller that
				// ignored it would otherwise reason about a real-looking zero.
				if left != 0 || right != 0 {
					t.Errorf("counts = %d/%d alongside an error, expected 0/0", left, right)
				}
				return
			}

			if err != nil {
				t.Fatalf("%q: %v", testCase.output, err)
			}
			if left != testCase.left || right != testCase.right {
				t.Errorf("counts = %d/%d, expected %d/%d", left, right, testCase.left, testCase.right)
			}
		})
	}
}
