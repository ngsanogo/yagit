package git_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The remote parsing and the argument lists, without git.
//
// Two subjects. The first is what `git remote --verbose` says and what must
// never leave this package: a URL with a token in it is a password printed on
// a screen, and the only thing standing between the two is the redaction
// tested here. The second is the shape of the commands — the separator, the
// refspec, the two force flags — each of which is a claim about what git does
// with a string somebody typed.

func TestParseRemotesPairsTheTwoDirections(t *testing.T) {
	output := []byte("origin\thttps://example.test/ada/yagit.git (fetch)\n" +
		"origin\thttps://example.test/ada/yagit.git (push)\n" +
		"fork\thttps://example.test/bob/yagit.git (fetch)\n" +
		"fork\tgit@example.test:bob/yagit.git (push)\n")

	remotes, err := git.ParseRemotes(output)
	if err != nil {
		t.Fatalf("ParseRemotes: %v", err)
	}

	want := []git.Remote{
		{
			Name:     "origin",
			FetchURL: "https://example.test/ada/yagit.git",
			PushURL:  "https://example.test/ada/yagit.git",
		},
		{
			// The arrangement the two fields exist for: read over https,
			// written over ssh. A single URL would show one half of it.
			Name:     "fork",
			FetchURL: "https://example.test/bob/yagit.git",
			PushURL:  "git@example.test:bob/yagit.git",
		},
	}
	if !slices.Equal(remotes, want) {
		t.Errorf("parsed %+v, want %+v", remotes, want)
	}
}

func TestParseRemotesKeepsGitsOrder(t *testing.T) {
	// git sorts remotes by name. Re-sorting here would be a second opinion
	// about something already decided, and the sidebar would then disagree
	// with the terminal about which remote comes first.
	output := []byte("zulu\thttps://example.test/z.git (fetch)\nzulu\thttps://example.test/z.git (push)\n" +
		"alpha\thttps://example.test/a.git (fetch)\nalpha\thttps://example.test/a.git (push)\n")

	remotes, err := git.ParseRemotes(output)
	if err != nil {
		t.Fatalf("ParseRemotes: %v", err)
	}
	if len(remotes) != 2 || remotes[0].Name != "zulu" || remotes[1].Name != "alpha" {
		t.Errorf("parsed %+v, want zulu before alpha, as git printed them", remotes)
	}
}

func TestParseRemotesAnswersNothingForARepositoryWithNoRemote(t *testing.T) {
	remotes, err := git.ParseRemotes(nil)
	if err != nil {
		t.Fatalf("ParseRemotes: %v", err)
	}
	if len(remotes) != 0 {
		t.Errorf("parsed %+v from no output", remotes)
	}
}

func TestParseRemotesRefusesALineItCannotRead(t *testing.T) {
	// Refused rather than skipped. A line this function cannot read means the
	// output is not what it thinks it is, and a remote silently missing from
	// the list is a push destination missing from the screen.
	for name, output := range map[string]string{
		"no tab":       "origin https://example.test/x.git (fetch)\n",
		"no direction": "origin\thttps://example.test/x.git\n",
		"a third word": "origin\thttps://example.test/x.git (mirror)\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := git.ParseRemotes([]byte(output)); err == nil {
				t.Errorf("ParseRemotes(%q) succeeded, expected a refusal naming the line", output)
			}
		})
	}
}

// The whole point of the redaction: what reaches a screen, and what does not.
func TestParseRemotesRedactsCredentials(t *testing.T) {
	for name, testCase := range map[string]struct{ url, want string }{
		"a token used as a password": {
			url:  "https://ada:ghp_secret@example.test/ada/yagit.git",
			want: "https://ada:***@example.test/ada/yagit.git",
		},
		"a token used as a username": {
			// Nothing in the string says whether this is a name or a secret,
			// so the whole of it goes: guessing wrong the other way prints a
			// credential.
			url:  "https://ghp_secret@example.test/ada/yagit.git",
			want: "https://***@example.test/ada/yagit.git",
		},
		"an at sign inside the password": {
			url:  "https://ada:p@ss@example.test/ada/yagit.git",
			want: "https://ada:***@example.test/ada/yagit.git",
		},
		"no credentials at all": {
			url:  "https://example.test/ada/yagit.git",
			want: "https://example.test/ada/yagit.git",
		},
		"the scp-like form, which has nowhere to put one": {
			url:  "git@example.test:ada/yagit.git",
			want: "git@example.test:ada/yagit.git",
		},
		"ssh, where the user name is not a secret": {
			url:  "ssh://git@example.test/ada/yagit.git",
			want: "ssh://***@example.test/ada/yagit.git",
		},
		"a local path": {
			url:  "/srv/git/yagit.git",
			want: "/srv/git/yagit.git",
		},
		"a host with no path": {
			url:  "https://ada:secret@example.test",
			want: "https://ada:***@example.test",
		},
	} {
		t.Run(name, func(t *testing.T) {
			remotes, err := git.ParseRemotes([]byte("origin\t" + testCase.url + " (fetch)\n"))
			if err != nil {
				t.Fatalf("ParseRemotes: %v", err)
			}
			if remotes[0].FetchURL != testCase.want {
				t.Errorf("shown as %q, want %q", remotes[0].FetchURL, testCase.want)
			}
			if strings.Contains(remotes[0].FetchURL, "secret") {
				t.Errorf("%q still carries the credential", remotes[0].FetchURL)
			}
		})
	}
}

func TestFetchArgs(t *testing.T) {
	// Every remote when none is named: a fetch button with no picker beside it
	// cannot mean one of them. --progress rides the request (ADR 0030).
	if got, want := git.FetchArgs(""), []string{"fetch", "--all", "--prune", "--progress"}; !slices.Equal(got, want) {
		t.Errorf("FetchArgs(\"\") = %v, want %v", got, want)
	}

	// And the separator, because `git remote add` accepts a remote called -x.
	if got, want := git.FetchArgs("-x"), []string{"fetch", "--prune", "--progress", "--", "-x"}; !slices.Equal(got, want) {
		t.Errorf("FetchArgs(\"-x\") = %v, want %v", got, want)
	}
}

func TestRemoteManagementArgsPutNamesAfterTheSeparator(t *testing.T) {
	if got, want := git.AddRemoteArgs("-f", "https://example.test/x.git"),
		[]string{"remote", "add", "--", "-f", "https://example.test/x.git"}; !slices.Equal(got, want) {
		t.Errorf("AddRemoteArgs = %v, want %v", got, want)
	}
	if got, want := git.RenameRemoteArgs("origin", "upstream"),
		[]string{"remote", "rename", "--", "origin", "upstream"}; !slices.Equal(got, want) {
		t.Errorf("RenameRemoteArgs = %v, want %v", got, want)
	}
	if got, want := git.RemoveRemoteArgs("-x"),
		[]string{"remote", "remove", "--", "-x"}; !slices.Equal(got, want) {
		t.Errorf("RemoveRemoteArgs = %v, want %v", got, want)
	}
	if got, want := git.SetRemoteURLArgs("origin", "https://example.test/x.git"),
		[]string{"remote", "set-url", "--", "origin", "https://example.test/x.git"}; !slices.Equal(got, want) {
		t.Errorf("SetRemoteURLArgs = %v, want %v", got, want)
	}
	if got, want := git.SetUpstreamArgs("main", "origin", "trunk"),
		[]string{"branch", "--set-upstream-to=origin/trunk", "--", "main"}; !slices.Equal(got, want) {
		t.Errorf("SetUpstreamArgs = %v, want %v", got, want)
	}
	if got, want := git.UnsetUpstreamArgs("main"),
		[]string{"branch", "--unset-upstream", "--", "main"}; !slices.Equal(got, want) {
		t.Errorf("UnsetUpstreamArgs = %v, want %v", got, want)
	}
}

func TestPullArgsAlwaysSaysWhichStrategyItIs(t *testing.T) {
	// Including the one that matches git's default. Leaving it out would let
	// pull.rebase in the user's configuration decide, and the button on screen
	// said which of the three this was.
	for strategy, flag := range map[git.PullStrategy]string{
		git.PullFastForward: "--ff-only",
		git.PullMerge:       "--no-rebase",
		git.PullRebase:      "--rebase",
	} {
		t.Run(string(strategy), func(t *testing.T) {
			args := git.PullArgs("origin", "trunk", strategy)
			want := []string{"pull", "--progress", flag, "--", "origin", "trunk"}
			if !slices.Equal(args, want) {
				t.Errorf("PullArgs = %v, want %v", args, want)
			}
		})
	}
}

func TestParsePullStrategyRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, raw := range []string{"", "ours", "FF-ONLY", "true"} {
		if _, err := git.ParsePullStrategy(raw); err == nil {
			t.Errorf("ParsePullStrategy(%q) was accepted; an unknown strategy must not read as a default", raw)
		}
	}

	for _, raw := range []string{"ff-only", "merge", "rebase"} {
		if _, err := git.ParsePullStrategy(raw); err != nil {
			t.Errorf("ParsePullStrategy(%q): %v", raw, err)
		}
	}
}

func TestPushArgsWritesTheRefspecOut(t *testing.T) {
	// A branch called main that follows origin/trunk pushes to trunk.
	// `git push origin main` would create a second branch on the server, and
	// push there forever after.
	args := git.PushArgs(git.PushDestination{
		Remote: "origin",
		Branch: "main",
		Ref:    "refs/heads/trunk",
	}, false)

	want := []string{"push", "--progress", "--", "origin", "main:refs/heads/trunk"}
	if !slices.Equal(args, want) {
		t.Errorf("PushArgs = %v, want %v", args, want)
	}
}

func TestPushArgsPublishesUnderTheBranchesOwnName(t *testing.T) {
	args := git.PushArgs(git.PushDestination{
		Remote:      "origin",
		Branch:      "feature",
		Ref:         "refs/heads/feature",
		SetUpstream: true,
	}, false)

	want := []string{"push", "--progress", "--set-upstream", "--", "origin", "feature:refs/heads/feature"}
	if !slices.Equal(args, want) {
		t.Errorf("PushArgs = %v, want %v", args, want)
	}
}

func TestPushArgsNeverForcesBare(t *testing.T) {
	args := git.PushArgs(git.PushDestination{
		Remote: "origin",
		Branch: "main",
		Ref:    "refs/heads/main",
	}, true)

	if slices.Contains(args, "--force") || slices.Contains(args, "-f") {
		t.Fatalf("PushArgs forced without a lease: %v", args)
	}
	// Both, and the second is not decoration: the lease is held against the
	// remote-tracking ref, which a fetch moves without anybody reading what
	// arrived. --force-if-includes is what requires the overwritten commits to
	// actually be in this branch's history.
	for _, flag := range []string{"--force-with-lease", "--force-if-includes"} {
		if !slices.Contains(args, flag) {
			t.Errorf("PushArgs = %v, missing %s", args, flag)
		}
	}
}

func TestDestinationForFollowsTheUpstreamOverTheSuggestion(t *testing.T) {
	// The remote the caller suggested is ignored when the branch already
	// follows something. Second-guessing the follow is how a repository ends
	// up with two branches for one line of work.
	destination, err := git.DestinationFor("main",
		git.Upstream{Remote: "origin", Ref: "refs/heads/trunk"}, "fork")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}

	want := git.PushDestination{Remote: "origin", Branch: "main", Ref: "refs/heads/trunk"}
	if destination != want {
		t.Errorf("DestinationFor = %+v, want %+v", destination, want)
	}
}

func TestDestinationForPublishesWhereItIsTold(t *testing.T) {
	destination, err := git.DestinationFor("feature", git.Upstream{}, "fork")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}

	want := git.PushDestination{
		Remote:      "fork",
		Branch:      "feature",
		Ref:         "refs/heads/feature",
		SetUpstream: true,
	}
	if destination != want {
		t.Errorf("DestinationFor = %+v, want %+v", destination, want)
	}
}

func TestDestinationForRefusesToChooseARemote(t *testing.T) {
	if _, err := git.DestinationFor("feature", git.Upstream{}, ""); !errors.Is(err, git.ErrNoRemote) {
		t.Errorf("DestinationFor with no upstream and no remote: %v, want ErrNoRemote", err)
	}

	if _, err := git.DestinationFor("", git.Upstream{}, "origin"); !errors.Is(err, git.ErrDetachedHEAD) {
		t.Errorf("DestinationFor with no branch: %v, want ErrDetachedHEAD", err)
	}
}

func TestUpstreamBranchIsTheShortNameOnTheOtherSide(t *testing.T) {
	upstream := git.Upstream{Remote: "origin", Ref: "refs/heads/feat/lanes"}
	if got := upstream.Branch(); got != "feat/lanes" {
		t.Errorf("Branch() = %q, want feat/lanes", got)
	}
	if !upstream.Configured() {
		t.Error("Configured() is false for a branch that follows origin/feat/lanes")
	}
	if (git.Upstream{}).Configured() {
		t.Error("Configured() is true for the zero upstream")
	}
}
