package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// What version of git is on this machine, for the two places it decides
// something.
//
// Asked rather than assumed, and asked for one reason only: a flag that an
// older git refuses by name. yagit drives the git already installed
// (ADR 0002), which means the range of versions is somebody else's decision,
// and a feature built on a flag from 2022 has to know whether it may use it.
//
// This is NOT a feature-detection framework, and it should not grow into one.
// The rule is the one the README states: a command that needs a newer git
// fails loudly on that command and nowhere else. A version is read here only
// where the alternative is worse than failing loudly — where an older git
// would put a permanent error on a screen that is always drawn.

// Version is a git version, compared rather than shown.
//
// Two numbers, because two is what the decisions here turn on. Git's own
// version strings carry more — a patch level, and on some platforms a vendor
// suffix such as "(Apple Git-154)" — and none of it has ever decided whether a
// flag exists.
type Version struct {
	Major int
	Minor int
}

// AtLeast reports whether this git is major.minor or newer.
func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

// String spells the version back the way it was read.
func (v Version) String() string { return fmt.Sprintf("%d.%d", v.Major, v.Minor) }

// GitVersion asks git what it is, once per Runner.
//
// Cached because it cannot change while the daemon runs: the binary is
// resolved at startup and a git upgraded underneath a running process is not a
// case worth a subprocess on every worktree list. A FAILURE is not cached —
// git being briefly unreachable should not make every later call answer from a
// remembered "no".
func (r *Runner) GitVersion(ctx context.Context) (Version, error) {
	r.versionMutex.Lock()
	defer r.versionMutex.Unlock()

	if r.versionRead {
		return r.version, nil
	}

	output, err := r.Run(ctx, "", "version")
	if err != nil {
		return Version{}, err
	}

	version, err := ParseVersion(string(output))
	if err != nil {
		return Version{}, err
	}

	r.version, r.versionRead = version, true
	return version, nil
}

// ParseVersion reads `git version 2.39.5 (Apple Git-154)`.
//
// Pure and exported for the reason ParseLog is: the vendor suffixes are real
// and varied, and a parser that tripped over one would disable a feature on
// the platform that ships it rather than crash where somebody would notice.
//
// Anything after the two numbers is ignored — the patch level, a release
// candidate's `-rc1`, a distribution's own tail. Nothing here has ever needed
// them, and reading them would be inventing a comparison to get wrong.
func ParseVersion(output string) (Version, error) {
	fields := strings.Fields(strings.TrimSpace(output))
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return Version{}, fmt.Errorf("git version: cannot read %q", strings.TrimSpace(output))
	}

	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return Version{}, fmt.Errorf("git version: cannot read %q", fields[2])
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("git version: cannot read the major number of %q", fields[2])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("git version: cannot read the minor number of %q", fields[2])
	}

	return Version{Major: major, Minor: minor}, nil
}

// versionCache is the Runner's memory of the answer. Its own type so the
// Runner's fields say what they are for rather than trailing three bare names.
type versionCache struct {
	versionMutex sync.Mutex
	version      Version
	versionRead  bool
}
