package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestMergeToolArgsPinsNoPromptAndALiteralPath(t *testing.T) {
	line := git.CommandLine(git.MergeToolArgs("app/[id].tsx"))
	if line != "git mergetool -y -- ':(literal)app/[id].tsx'" {
		t.Fatalf("command = %q", line)
	}
}

func TestMergeToolArgsKeepsASimplePathReadable(t *testing.T) {
	line := git.CommandLine(git.MergeToolArgs("f.txt"))
	if !strings.Contains(line, "mergetool -y --") {
		t.Fatalf("command = %q", line)
	}
	if !strings.Contains(line, ":(literal)f.txt") {
		t.Fatalf("path was not literal: %q", line)
	}
}
