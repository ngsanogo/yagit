package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Code is one character of git's two-letter status field: the state of a path
// in the index (X) or in the work tree (Y).
//
// A string rather than a byte so that it crosses to JSON as "M" and not as
// 77, which is the sort of field an interface silently renders as a number.
type Code string

const (
	// CodeUnchanged is git's '.', which the porcelain v2 format writes where
	// the short format leaves a space. Nothing changed on that side.
	CodeUnchanged Code = "."

	CodeModified   Code = "M"
	CodeAdded      Code = "A"
	CodeDeleted    Code = "D"
	CodeRenamed    Code = "R"
	CodeCopied     Code = "C"
	CodeUnmerged   Code = "U"
	CodeTypeChange Code = "T"
)

// EntryKind is the shape of a status record, which decides how its two codes
// are to be read. The codes of an unmerged entry are not index-and-work-tree
// at all: they are us-and-them.
type EntryKind string

const (
	EntryOrdinary  EntryKind = "ordinary"
	EntryRenamed   EntryKind = "renamed"
	EntryCopied    EntryKind = "copied"
	EntryUnmerged  EntryKind = "unmerged"
	EntryUntracked EntryKind = "untracked"
)

// SubmoduleState is the detail git gives about a path that is a submodule.
// Nil for anything that is not one.
type SubmoduleState struct {
	// CommitChanged: the submodule's HEAD is not the commit recorded here.
	CommitChanged bool `json:"commit_changed"`
	// Modified: the submodule has modified tracked files.
	Modified bool `json:"modified"`
	// Untracked: the submodule has untracked files.
	Untracked bool `json:"untracked"`
}

// FileStatus is one path git reports as differing from HEAD, from the index,
// or from both.
//
// Index and WorkTree are the raw codes rather than a single "status" verb,
// and that is deliberate: a file can be added to the index and modified again
// afterwards, which is one path with two different answers. Collapsing them
// into one word is how an interface ends up staging something other than what
// it showed.
type FileStatus struct {
	Path string    `json:"path"`
	Kind EntryKind `json:"kind"`

	Index    Code `json:"index"`
	WorkTree Code `json:"work_tree"`

	// OldPath is where a renamed or copied path came from. Empty otherwise.
	OldPath string `json:"old_path,omitempty"`

	// Score is git's similarity percentage for a rename or a copy.
	Score int `json:"score,omitempty"`

	Submodule *SubmoduleState `json:"submodule,omitempty"`
}

// Staged says the path differs between HEAD and the index — there is
// something in it that a commit would record.
func (f FileStatus) Staged() bool {
	return f.Kind != EntryUntracked && f.Kind != EntryUnmerged && f.Index != CodeUnchanged
}

// Unstaged says the path differs between the index and the work tree. An
// untracked file counts: it is a change the work tree has and the index does
// not.
func (f FileStatus) Unstaged() bool {
	return f.Kind == EntryUntracked || f.Kind == EntryUnmerged || f.WorkTree != CodeUnchanged
}

// Conflict describes an unmerged path in words.
//
// The pair of codes is exact and unreadable — "DU" is a fact about two index
// stages, not a sentence anybody can act on. This is the sentence, and it is
// built here rather than in the interface so that the daemon and the log
// agree on the wording.
type Conflict string

const (
	ConflictBothModified  Conflict = "both modified"
	ConflictBothAdded     Conflict = "both added"
	ConflictBothDeleted   Conflict = "both deleted"
	ConflictAddedByUs     Conflict = "added by us"
	ConflictAddedByThem   Conflict = "added by them"
	ConflictDeletedByUs   Conflict = "deleted by us"
	ConflictDeletedByThem Conflict = "deleted by them"
)

// Conflict returns how a path is unmerged, or the empty string when it is
// not.
func (f FileStatus) Conflict() Conflict {
	if f.Kind != EntryUnmerged {
		return ""
	}
	switch string(f.Index) + string(f.WorkTree) {
	case "UU":
		return ConflictBothModified
	case "AA":
		return ConflictBothAdded
	case "DD":
		return ConflictBothDeleted
	case "AU":
		return ConflictAddedByUs
	case "UA":
		return ConflictAddedByThem
	case "DU":
		return ConflictDeletedByUs
	case "UD":
		return ConflictDeletedByThem
	default:
		// git documents exactly the seven pairs above. An eighth would be a
		// new git; naming it beats dropping the file from the list.
		return Conflict("unmerged (" + string(f.Index) + string(f.WorkTree) + ")")
	}
}

// Status is the whole answer to `git status`: where HEAD is, how it stands
// against its upstream, and every path that differs.
type Status struct {
	// Branch is the branch HEAD is on. Empty when HEAD is detached, and
	// Detached says which of the two it is — an empty name is not a state the
	// interface should have to infer.
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`

	// HeadSHA is the commit HEAD points at. Empty on an unborn branch, the
	// state right after `git init`, which Unborn names explicitly.
	HeadSHA string `json:"head_sha"`
	Unborn  bool   `json:"unborn"`

	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`

	Files []FileStatus `json:"files"`
}

// Clean says nothing differs anywhere: no staged change, no unstaged change,
// no untracked file.
func (s Status) Clean() bool { return len(s.Files) == 0 }

// DirtyTracked counts the paths git already knows about that differ from HEAD
// or from the index.
//
// Untracked files are excluded, and that exclusion is the whole point of the
// method: `git status` is read with --untracked-files=all, so an untracked
// directory of five hundred files answers five hundred, while the operations
// that ask this question do not touch a single one of them. A hard reset
// leaves untracked files exactly where they are; a stash being applied cannot
// collide with one, because git refuses to overwrite it and says so.
//
// So a count that included them would put a number under "this will
// permanently discard", or under "this might conflict", for work that is in no
// danger at all — which is how a warning stops being read.
//
// An unmerged path IS counted: a hard reset clears it, and a stash applied
// over one is a conflict on top of a conflict.
func (s Status) DirtyTracked() int {
	count := 0
	for _, file := range s.Files {
		if file.Kind != EntryUntracked {
			count++
		}
	}
	return count
}

// Conflicted says the repository is in the middle of a merge that did not
// resolve on its own.
func (s Status) Conflicted() bool {
	for _, file := range s.Files {
		if file.Kind == EntryUnmerged {
			return true
		}
	}
	return false
}

// statusArgs is the command, written once.
//
// --untracked-files=all rather than the default: git otherwise collapses an
// untracked directory into a single "sub/" entry, and a panel that offers to
// stage "sub/" cannot show what is inside it or let a file be left out. The
// cost is that a repository with a large untracked tree and no .gitignore
// covering it makes git walk that tree — which is the same cost `git status`
// pays in a terminal, and the same fix.
//
// --branch adds the header lines: which branch, which commit, how far from
// the upstream. They arrive in the same read as the files, so the panel never
// shows a branch name from one moment and a file list from another.
//
// -z is what makes the output parseable at all: without it git quotes and
// backslash-escapes any path holding a space or a non-ASCII byte, and the
// interface would show a path that is not the path.
var statusArgs = []string{
	"status", "--porcelain=v2", "--branch", "--untracked-files=all", "-z",
}

// Status reads the working directory.
func (r *Runner) Status(ctx context.Context, dir string) (Status, error) {
	output, err := r.Run(ctx, dir, statusArgs...)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(output)
}

// ParseStatus turns `git status --porcelain=v2 -z` output into a Status.
//
// Pure and exported: this is one of the places where a bug would be silent,
// so it is testable without running git.
func ParseStatus(output []byte) (Status, error) {
	status := Status{Files: []FileStatus{}}

	// The records are NUL-TERMINATED, not NUL-separated: the last one ends
	// with a NUL too, so splitting leaves a trailing empty chunk. Trimming it
	// beforehand keeps the loop from having to know that.
	fields := splitNulTerminated(output)

	for index := 0; index < len(fields); index++ {
		field := fields[index]
		if field == "" {
			continue
		}

		switch field[0] {
		case '#':
			if err := parseStatusHeader(&status, field); err != nil {
				return Status{}, err
			}

		case '1':
			file, err := parseOrdinaryEntry(field)
			if err != nil {
				return Status{}, err
			}
			status.Files = append(status.Files, file)

		case '2':
			// A rename or a copy spends two records: the header line, then
			// the path it came from. Reading one without the other shifts
			// every field after it, which is why the shortage is an error
			// rather than an empty OldPath.
			if index+1 >= len(fields) {
				return Status{}, fmt.Errorf(
					"rename entry %q has no origin path after it", field)
			}
			index++
			file, err := parseRenameEntry(field, fields[index])
			if err != nil {
				return Status{}, err
			}
			status.Files = append(status.Files, file)

		case 'u':
			file, err := parseUnmergedEntry(field)
			if err != nil {
				return Status{}, err
			}
			status.Files = append(status.Files, file)

		case '?':
			status.Files = append(status.Files, FileStatus{
				Path:     strings.TrimPrefix(field, "? "),
				Kind:     EntryUntracked,
				Index:    CodeUnchanged,
				WorkTree: CodeUnchanged,
			})

		case '!':
			// Ignored paths. Not asked for — statusArgs does not pass
			// --ignored — so reaching this means a caller changed the
			// command; skipping is right either way, since an ignored file is
			// not a change.

		default:
			return Status{}, fmt.Errorf("unknown status record %q", field)
		}
	}

	return status, nil
}

// splitNulTerminated cuts NUL-terminated records, dropping the empty tail the
// final terminator leaves behind.
func splitNulTerminated(output []byte) []string {
	text := strings.TrimSuffix(string(output), fieldSeparator)
	if text == "" {
		return nil
	}
	return strings.Split(text, fieldSeparator)
}

// parseStatusHeader reads one `# key value` line.
//
// An unknown header is ignored rather than refused: git adds them over time —
// `# stash` arrived in 2.35 — and one unread line is a smaller failure than a
// working directory that will not load on a newer git than the one this was
// written against.
func parseStatusHeader(status *Status, field string) error {
	key, value, found := strings.Cut(strings.TrimPrefix(field, "# "), " ")
	if !found {
		return nil
	}

	switch key {
	case "branch.oid":
		// git writes the literal "(initial)" on a branch with no commit yet.
		if value == "(initial)" {
			status.Unborn = true
			return nil
		}
		status.HeadSHA = value

	case "branch.head":
		if value == "(detached)" {
			status.Detached = true
			return nil
		}
		status.Branch = value

	case "branch.upstream":
		status.Upstream = value

	case "branch.ab":
		ahead, behind, err := parseAheadBehind(value)
		if err != nil {
			return err
		}
		status.Ahead, status.Behind = ahead, behind
	}

	return nil
}

// parseAheadBehind reads the "+3 -2" of a `# branch.ab` header.
//
// Refused rather than defaulted when it does not parse: this is the number
// the interface puts on a push button, and a silent zero would say "nothing
// to push" about a branch that is three commits ahead.
func parseAheadBehind(value string) (ahead, behind int, err error) {
	aheadField, behindField, found := strings.Cut(value, " ")
	if !found {
		return 0, 0, fmt.Errorf("branch.ab header %q is not two fields", value)
	}

	ahead, err = strconv.Atoi(strings.TrimPrefix(aheadField, "+"))
	if err != nil {
		return 0, 0, fmt.Errorf("branch.ab ahead count %q: %w", aheadField, err)
	}

	behind, err = strconv.Atoi(strings.TrimPrefix(behindField, "-"))
	if err != nil {
		return 0, 0, fmt.Errorf("branch.ab behind count %q: %w", behindField, err)
	}

	// git writes the behind count as a negative number. The interface counts
	// commits, and there is no such thing as minus two of them.
	if behind < 0 {
		behind = -behind
	}
	return ahead, behind, nil
}

// The field layouts, from git's status documentation. Every entry begins with
// its kind, then the two status codes, then the submodule field, and the
// counts differ from there.
//
//	1 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <path>
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <path>
//	u <xy> <sub> <m1> <m2> <m3> <mW> <h1> <h2> <h3> <path>
//
// The path is the last field and may hold spaces, so it is taken as the
// remainder rather than as one more split — the reason every parser below
// uses SplitN with a fixed count.
const (
	ordinaryFields = 9
	renameFields   = 10
	unmergedFields = 11
)

func parseOrdinaryEntry(field string) (FileStatus, error) {
	parts := strings.SplitN(field, " ", ordinaryFields)
	if len(parts) != ordinaryFields {
		return FileStatus{}, fmt.Errorf(
			"ordinary status entry %q has %d fields, expected %d",
			field, len(parts), ordinaryFields)
	}

	index, workTree, err := parseCodePair(parts[1])
	if err != nil {
		return FileStatus{}, fmt.Errorf("ordinary status entry %q: %w", field, err)
	}

	return FileStatus{
		Path:      parts[8],
		Kind:      EntryOrdinary,
		Index:     index,
		WorkTree:  workTree,
		Submodule: parseSubmoduleField(parts[2]),
	}, nil
}

func parseRenameEntry(field, oldPath string) (FileStatus, error) {
	parts := strings.SplitN(field, " ", renameFields)
	if len(parts) != renameFields {
		return FileStatus{}, fmt.Errorf(
			"rename status entry %q has %d fields, expected %d",
			field, len(parts), renameFields)
	}

	index, workTree, err := parseCodePair(parts[1])
	if err != nil {
		return FileStatus{}, fmt.Errorf("rename status entry %q: %w", field, err)
	}

	// The field is "R100" or "C75": the operation, then the similarity.
	operation := parts[8]
	kind := EntryRenamed
	if strings.HasPrefix(operation, "C") {
		kind = EntryCopied
	}

	// A score git wrote in a shape we do not recognize is not worth failing
	// the whole read for: it decorates the row, and the paths — which is what
	// staging acts on — are right either way. parseCountOrZero, beside the
	// ref parser, is the same judgement made for the same reason.
	score := parseCountOrZero(strings.TrimLeft(operation, "RC"))

	return FileStatus{
		Path:      parts[9],
		OldPath:   oldPath,
		Kind:      kind,
		Index:     index,
		WorkTree:  workTree,
		Score:     score,
		Submodule: parseSubmoduleField(parts[2]),
	}, nil
}

func parseUnmergedEntry(field string) (FileStatus, error) {
	parts := strings.SplitN(field, " ", unmergedFields)
	if len(parts) != unmergedFields {
		return FileStatus{}, fmt.Errorf(
			"unmerged status entry %q has %d fields, expected %d",
			field, len(parts), unmergedFields)
	}

	us, them, err := parseCodePair(parts[1])
	if err != nil {
		return FileStatus{}, fmt.Errorf("unmerged status entry %q: %w", field, err)
	}

	return FileStatus{
		Path: parts[10],
		Kind: EntryUnmerged,
		// For an unmerged path these two are "us" and "them", not index and
		// work tree. The field names are kept so the record stays one shape;
		// Conflict() is what turns the pair into something readable.
		Index:     us,
		WorkTree:  them,
		Submodule: parseSubmoduleField(parts[2]),
	}, nil
}

func parseCodePair(field string) (Code, Code, error) {
	if len(field) != 2 {
		return "", "", fmt.Errorf("status code %q is not two characters", field)
	}
	return Code(field[0:1]), Code(field[1:2]), nil
}

// parseSubmoduleField reads git's four-character submodule field: "N..." for
// a path that is not a submodule, "S<c><m><u>" for one that is.
func parseSubmoduleField(field string) *SubmoduleState {
	if !strings.HasPrefix(field, "S") || len(field) != 4 {
		return nil
	}
	return &SubmoduleState{
		CommitChanged: field[1] == 'C',
		Modified:      field[2] == 'M',
		Untracked:     field[3] == 'U',
	}
}
