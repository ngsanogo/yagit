package graph_test

import (
	"slices"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/graph"
)

// Lane assignment is the second of the two places in yagit where a bug is
// silent: a wrong column does not crash, it draws a plausible graph of a
// history that does not exist. The cases beside this file pin the topologies
// somebody thought of — a merge, an octopus, a criss-cross. This one goes
// looking for the ones nobody did.
//
// The fuzzer does not hand bytes to Assign. It hands them to a builder that
// turns any input at all into a well-formed history, so every failure is a
// real defect rather than a violated precondition. What is being explored is
// the shape of the graph, not the parsing of anything.
//
// `go test` runs the seed corpus on every run, so these are regression tests
// for free. `./do test fuzz` is what actually goes looking.
func FuzzAssign(f *testing.F) {
	f.Add([]byte{0x00})                                     // a lone root
	f.Add([]byte{0x11, 0x11, 0x11})                         // a straight line
	f.Add([]byte{0x21, 0x11, 0x11, 0x00})                   // a branch and a merge
	f.Add([]byte{0x31, 0x21, 0x11, 0x41, 0x11, 0x00, 0x00}) // an octopus over a fork
	f.Add([]byte{0x11, 0x00, 0x11, 0x00, 0x11, 0x00})       // unrelated roots

	f.Fuzz(func(t *testing.T, shape []byte) {
		commits := historyFromShape(shape)
		assigned := graph.Assign(commits)
		checkInvariants(t, commits, assigned)
	})
}

// historyFromShape turns arbitrary bytes into a history that is always valid:
// unique SHAs, and parents that are always older than their children, which is
// the topological order Assign requires.
//
// One byte per commit. The low nibble is how many parents it has, capped at
// four so an octopus is reachable without every input becoming one; the high
// nibble is how far back the first of them reaches. The others reach back by
// the bytes that follow, which is what lets the fuzzer build a branch that
// lives for a long time before it is absorbed — the case that costs a column.
func historyFromShape(shape []byte) []git.Commit {
	// Small enough that a fuzz run explores many shapes rather than a few
	// large ones, and large enough for a branch to outlive several merges.
	const most = 64

	commits := make([]git.Commit, 0, min(len(shape), most))
	for position, choice := range shape {
		if len(commits) == most {
			break
		}

		index := len(commits)
		count := min(int(choice&0x0f), 4)
		if index == 0 {
			count = 0 // the first commit has nothing to descend from
		}

		var parents []string
		reach := int(choice >> 4)
		for parent := range count {
			// Every step consumes a further byte of the input, so the reaches
			// of an octopus are independent rather than all equal.
			if parent > 0 {
				reach = int(shape[(position+parent)%len(shape)])
			}
			parents = append(parents, name(index-1-reach%index))
		}
		commits = append(commits, git.Commit{SHA: name(index), Parents: parents})
	}

	// Built oldest first so a parent can only be an existing commit; reversed
	// because Assign is given children before parents.
	slices.Reverse(commits)
	return commits
}
