package api

import "testing"

// gitFloors is read in order to build a list of sentences, so a floor out of
// order reads as nonsense. The numbers are also the contract the README
// states, which is why one changed here without changing that is a drift
// nothing else would catch.
func TestGitFloorsAreOrderedAndEachNamesAnOperation(t *testing.T) {
	for index, floor := range gitFloors {
		if floor.operation == "" {
			t.Errorf("floor %d names no operation, so the sentence it produces says nothing", index)
		}
		if index == 0 {
			continue
		}
		previous := gitFloors[index-1]
		if floor.major < previous.major || (floor.major == previous.major && floor.minor < previous.minor) {
			t.Errorf("floors are out of order: %d.%d comes after %d.%d",
				floor.major, floor.minor, previous.major, previous.minor)
		}
	}
}
