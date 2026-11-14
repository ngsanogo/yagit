package history_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points git at empty configuration files for the whole binary.
//
// The Runner passes HOME through on purpose, so git would read whatever
// ~/.gitconfig the machine has: a global init.templateDir, core.hooksPath or
// commit.gpgsign changes what these repositories come out as, and the suite
// would pass or fail on whose machine ran it. GIT_CONFIG_GLOBAL and
// GIT_CONFIG_SYSTEM are git's way to say "read nothing at this level". Set
// here once rather than per test with t.Setenv, which would forbid the tests
// from running in parallel.
func TestMain(m *testing.M) {
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(name, os.DevNull); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}
