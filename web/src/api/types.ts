/**
 * The contract between the daemon and the interface.
 *
 * These types mirror the Go structures in internal/git and internal/repo
 * exactly. They are written out here once, by hand: generating them would add
 * one more tool to the chain for a handful of structures that rarely change.
 */

export interface Commit {
  sha: string;
  parents: string[];
  author: string;
  /** Author date, in RFC 3339 format. */
  date: string;
  subject: string;
  /** Git ref decoration: "HEAD -> main", "origin/main", "tag: v1.0". */
  refs: string[];
}

/**
 * Commits that touched one path, following renames.
 *
 * `revision` is where the walk started — a full object name, or HEAD when the
 * client left it empty. `limit` is the daemon's ceiling, not a page size the
 * client picks.
 */
export interface FileHistory {
  path: string;
  revision: string;
  limit: number;
  commits: Commit[];
}

/** One annotated line from `git blame --line-porcelain`. */
export interface BlameLine {
  number: number;
  text: string;
  sha: string;
  author: string;
  /** Author date, in RFC 3339 format. */
  date: string;
  subject: string;
}

/** Who last touched each line of a path at a revision. */
export interface Blame {
  path: string;
  revision: string;
  lines: BlameLine[];
}

/**
 * Commits that changed one line of one path.
 *
 * `line` is 1-based, matching blame. `limit` is the daemon's ceiling.
 */
export interface LineHistory {
  path: string;
  revision: string;
  line: number;
  limit: number;
  commits: Commit[];
}

/**
 * A commit with the column its dot sits in.
 *
 * The column comes from the daemon rather than being worked out here, and it
 * has to: a commit's column follows from every commit above it, so it cannot
 * be derived from a page. See docs/adr/0012.
 */
export interface CommitRow extends Commit {
  lane: number;
}

/**
 * The row a line points at when its parent is not in the history — a shallow
 * clone, a history cut short. The line is real and it leaves the bottom of the
 * picture, which is what gets drawn rather than pretending the commit is a
 * root.
 */
export const ABSENT_ROW = -1;

/**
 * One line of the graph: from a commit's dot, down a single column, to one of
 * its parents' dots.
 *
 * Rows are indices into the whole history, never into the page. That is what
 * lets a page be drawn on its own — a line crossing it has both of its ends
 * somewhere else, and their columns travel with it for the same reason.
 */
export interface GraphEdge {
  from: number;
  from_lane: number;
  /** ABSENT_ROW when the parent is not in the history. */
  to: number;
  /** ABSENT_ROW likewise. */
  to_lane: number;
  lane: number;
}

/**
 * Which refs the graph is drawn from.
 *
 * `head` is what is checked out — the current branch, or the commit a detached
 * HEAD sits on — and is the picture the interface asks for first. `all` is
 * every ref, which on a repository with a thousand tags is the three hundred
 * column graph nobody can read. See docs/adr/0014.
 *
 * `refs` is the middle neither of them reaches: the two or three references
 * somebody picked, and the graph between them. The chosen set IS the scope
 * there — main alone and main with a topic branch are two different histories,
 * not two views of one — which is why the refs travel with it everywhere.
 * See docs/adr/0033.
 */
export type HistoryScope = 'head' | 'all' | 'refs';

/**
 * One checkout of a repository — `git worktree`.
 *
 * A repository can have several: one object database, one set of refs, and a
 * directory per branch somebody wants open at once. yagit already understood
 * them before it could make one, because a linked worktree keeps HEAD and the
 * index of its own while sharing everything else.
 */
export interface Worktree {
  /** The directory it lives in — its identity, and what every command takes. */
  path: string;
  /** The commit checked out there. Empty for a bare repository. */
  head: string;
  /** Short branch name, empty when detached or bare. */
  branch: string;
  detached: boolean;
  bare: boolean;
  /** Marked as not-to-be-pruned, usually because the drive is not always there. */
  locked: boolean;
  lock_reason: string;
  /** git considers the administrative files stale: the directory is gone. */
  prunable: boolean;
  prunable_reason: string;
  /**
   * The work tree the repository was made in, which `remove` refuses.
   *
   * "Work tree" is the term the interface uses everywhere; git's own word for
   * this one in particular is "main working tree", and that is the phrase its
   * refusal prints, which is why the panel quotes it back rather than
   * translating it.
   */
  main: boolean;
  /** The checkout this tab is open on. */
  current: boolean;
}

/**
 * What making another checkout asks for.
 *
 * `ref` empty means HEAD, which is git's own default. `new_branch` makes the
 * branch there rather than checking out one that exists; `detach` is the only
 * way a tag or a commit can be checked out at all.
 */
export interface WorktreeRequest {
  path: string;
  ref: string;
  new_branch: string;
  detach: boolean;
}

/**
 * Another repository pinned inside this one at one commit.
 *
 * The list is built from the index rather than from `.gitmodules`, which is
 * why `declared` exists: a gitlink with no section describing it is a real
 * state — somebody committed the submodule and not the file that names it.
 */
export interface Submodule {
  /** The section name in `.gitmodules`, which need not be the path. */
  name: string;
  /** Where it sits in the work tree, repository-relative. */
  path: string;
  /** What `.gitmodules` declares, with any credentials redacted. */
  url: string;
  /** The commit this repository pins — the gitlink in the index. */
  recorded: string;
  /** The commit checked out inside it, empty when there is no checkout. */
  head: string;
  /** `git submodule init` has run: the URL is in this repository's config. */
  initialised: boolean;
  /** There is a checkout on disk at `path`. */
  present: boolean;
  /** The checkout is at a commit other than the one recorded. */
  moved: boolean;
  /** `.gitmodules` has a section for it. */
  declared: boolean;
}

/** Where a history search looks. */
export type SearchField = 'message' | 'author' | 'path' | 'content';

/**
 * What a search found.
 *
 * A list rather than a filtered graph: lane assignment draws how commits
 * CONNECT, and assigning over a filtered set draws connections the repository
 * does not have. Following a result takes the graph to that commit instead.
 */
export interface SearchResult {
  commits: Commit[];
  /** The repository holds more matches than the daemon's cap. */
  truncated: boolean;
  /** The exact line that ran, for a search that found nothing to explain. */
  command: string;
}

/** One page of a repository's history, with the part of the graph it needs. */
export interface CommitPage {
  commits: CommitRow[];
  edges: GraphEdge[];
  /** Index of `commits[0]` in the whole history. */
  first: number;
  /** Rows per page. The daemon decides it; the interface reads it here. */
  page_size: number;
  /** Commits in the whole history, however few of them this page holds. */
  total: number;
  /** Columns the picture needs, measured over the whole history so that the
   * graph does not change width as you scroll. */
  width: number;
}

/**
 * One commit, whole, and where it sits in the walk being drawn.
 *
 * `row` is half the reason this is a route of its own. It follows from the
 * whole assignment, so it cannot be worked out from a page — and the commit a
 * reference names is usually in none of the pages the interface holds. The
 * other half is the patch: two hundred of those is not a page anyone wants,
 * so they are asked for one commit at a time.
 *
 * Declared here rather than beside CommitDetail because the two fields below
 * are the only thing about a commit that depends on which walk it was found
 * in — everything else is a property of the object itself.
 */
export interface LocatedCommit extends CommitDetail {
  /** Its index in the walk: the row the list scrolls to. */
  row: number;
  /** The column its dot is drawn in. */
  lane: number;
}

export type RefKind = 'branch' | 'remote' | 'tag' | 'other';

/**
 * Where HEAD sits.
 *
 * `for-each-ref` lists no HEAD, so this is the only way the sidebar learns
 * which branch is checked out — the one fact in that list that moves under
 * the user's feet.
 */
export interface Head {
  sha: string;
  /** The short branch name, or "HEAD" when detached. */
  name: string;
  detached: boolean;
}

export interface RefsPayload {
  refs: Ref[];
  /** Absent in a repository with no commit yet. */
  head?: Head;
}

export interface Ref {
  name: string;
  short_name: string;
  kind: RefKind;
  sha: string;
  upstream?: string;
  ahead: number;
  behind: number;
  gone: boolean;
}

/**
 * One configured remote.
 *
 * Two URLs because remote.<name>.pushurl exists: a fork read over https and
 * written over ssh is an ordinary arrangement, and a single URL would show one
 * half of it. Both arrive with any credentials removed — see the daemon's
 * redactURL — so what is here is safe to draw and useless to git.
 */
export interface Remote {
  name: string;
  fetch_url: string;
  push_url: string;
}

/**
 * What to do with the commits a pull brings back.
 *
 * git answers this question with a setting, and warns when it is unset,
 * because the right answer depends on the repository and on the team. A button
 * cannot read a setting and still say what it does, so these are three
 * operations rather than one: `ff-only` is the one that can neither write a
 * commit nobody asked for nor stop halfway on a conflict, and is what the Pull
 * button runs. The other two are on the menu beside it, for the moment the
 * first one refuses.
 */
export type PullStrategy = 'ff-only' | 'merge' | 'rebase';

/**
 * What merging one branch into another would do.
 *
 * The same question as PullStrategy above and the opposite answer, because a
 * merge between two local branches has no question in it: which of the three
 * this is depends only on where the two branches stand, so the daemon reads it
 * and the browser is told. What the browser does with it is send it back —
 * it chooses the flag that pins the outcome, so the command in the dialog
 * cannot be turned into a different one by the merge.ff in somebody's
 * ~/.gitconfig.
 *
 * `fast-forward` writes no object at all: no commit, no hook, no signature.
 * `merge-commit` records one, with all three. `up-to-date` is the branch
 * already being contained, which is not an error and not a merge either.
 */
export type MergeOutcome = 'up-to-date' | 'fast-forward' | 'merge-commit';

/**
 * What a merge would do, and the line it would run.
 *
 * Answered by the daemon before the merge, for the reason PushPlan is: the
 * command has to be exact and the browser has no business assembling one. The
 * rest is the same reading in the terms a sentence needs — see mergeSummary.
 */
export interface MergePlan {
  /** The exact line, for the confirmation to show. */
  command: string;
  /** The branch being brought in. */
  branch: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the merge so
   * a destination that moved between the question and the answer is refused
   * rather than merged into.
   */
  into: string;
  outcome: MergeOutcome;
  /** Commits the current branch has that the other does not. */
  ahead: number;
  /** Commits the merge would bring in. */
  behind: number;
}

/**
 * What rebasing onto another branch would do.
 *
 * The same three a merge has, because the same fact decides both — how the two
 * branches stand to each other. `up-to-date` is the upstream already contained
 * in the branch, so git writes nothing. `fast-forward` is a branch with no
 * commit of its own, moved onto the upstream: nothing is replayed and no hash
 * changes. `rebase` writes the branch's commits again on a new base, under new
 * hashes and the user's hooks.
 */
export type RebaseOutcome = 'up-to-date' | 'fast-forward' | 'rebase';

/**
 * What a rebase would do, and the line it would run.
 *
 * Answered by the daemon before the rebase, for the reason MergePlan is: the
 * command has to be exact and the browser has no business assembling one.
 */
export interface RebasePlan {
  /** The exact line, for the confirmation to show. */
  command: string;
  /** The branch to replay onto. */
  onto: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the rebase so
   * a branch that moved between the question and the answer is refused.
   */
  from: string;
  outcome: RebaseOutcome;
  /**
   * Ordinary commits the branch holds past the fork. Every one of them stops
   * being what the branch points at — written again under a new hash, or
   * dropped by git as a patch it already has. Zero unless the outcome is
   * `rebase`.
   */
  rewriting: number;
  /**
   * Merge commits among them, discarded rather than recreated: a rebase
   * replays a straight line. Zero unless the outcome is `rebase`.
   */
  flattening: number;
  /** Commits the new base holds that the branch does not — how far to move. */
  behind: number;
}

/**
 * What one commit's row in a rebase plan does, spelled the way git spells it
 * in a todo list.
 *
 * The value is the verb git parses, so the label on the row, the value on the
 * wire and the line in the file git executes are one string. There is no table
 * mapping yagit's word onto git's, and so nothing for the two to drift apart
 * over.
 *
 * Five, and the set was chosen by one rule: every one of them commits under a
 * message that already exists. `squash` and `reword` are missing because both
 * ask git to open an editor over a message nobody has written yet, and the
 * daemon has no terminal to show it in — so combining is offered as the
 * question that HAS an answer, which is whose message survives.
 */
export type RebaseInstruction = 'pick' | 'fixup' | 'fixup -C' | 'edit' | 'drop';

/** One row of a rebase plan: a commit, and what to do with it. */
export interface RebaseStep {
  /** The full object name, as the daemon resolved it. */
  commit: string;
  instruction: RebaseInstruction;
}

/**
 * The commits a plan may be written over.
 *
 * Answered by the daemon before anything is arranged, and it answers with
 * material rather than with an outcome: an interactive rebase is a list the
 * user writes, so there is nothing to predict until they have written it.
 *
 * The commits are oldest first, which is the order a todo list is executed in.
 * Drawn in that order too, so the list on screen and the list git runs are one
 * list rather than two conventions for it.
 */
export interface InteractiveRebasePlan {
  /** The exact line, for the dialog to show beside the rows. */
  command: string;
  /** The full object name of the commit the plan starts AFTER. */
  base: string;
  /** That commit's subject: a plan is "the commits after this one". */
  subject: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the plan so
   * a branch that moved between the question and the answer is refused.
   */
  from: string;
  /** What the plan covers, oldest first. */
  commits: Commit[];
}

/**
 * What running a plan left behind: the references, and whether git stopped.
 *
 * The second half is not decoration. A plan holding an `edit` ends with git
 * waiting in the middle of it, having exited zero — nothing in a reference
 * list tells that from a rebase that finished, and a toast reporting success
 * over a repository sitting halfway through a rewrite is the worst thing this
 * interface could say.
 */
export interface InteractiveRebaseResult extends RefsPayload {
  /** True when git is waiting: at an `edit`, with the rest of the plan to go. */
  stopped?: boolean;
  /** How far it got. Only ever shown with `total` — "3" alone says nothing. */
  step?: number;
  total?: number;
}

/**
 * What cherry-picking one commit onto the current branch would do.
 *
 * `up-to-date` is the commit already on the branch — there is no command, and
 * the confirmation never opens. `fast-forward` is HEAD as the commit's parent,
 * so the branch moves onto it. `cherry-pick` records a new commit under the
 * user's hooks.
 */
export type CherryPickOutcome = 'up-to-date' | 'fast-forward' | 'cherry-pick';

/**
 * What a cherry-pick would do, and the line it would run.
 *
 * Answered by the daemon before the cherry-pick. `command` is empty when the
 * outcome is `up-to-date`: there is no cherry-pick that succeeds as a no-op,
 * and inventing one would put a lie on the confirmation.
 */
export interface CherryPickPlan {
  /** The exact line, or empty when nothing would run. */
  command: string;
  /** The full object name the daemon resolved. */
  commit: string;
  /** The commit's subject, for a confirmation that names more than a hash. */
  subject: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the
   * cherry-pick so a destination that moved is refused.
   */
  into: string;
  outcome: CherryPickOutcome;
}

/**
 * What reverting one commit on the current branch would do.
 *
 * One value today: a revert always records a new commit. Kept as a named
 * outcome so an unknown string from a newer or older daemon is refused rather
 * than defaulted.
 */
export type RevertOutcome = 'revert';

/**
 * What a revert would do, and the line it would run.
 *
 * Answered by the daemon before the revert. The command always carries
 * `--no-edit`, without which git opens an editor the daemon has nowhere to
 * show, and `--no-reference`, without which `revert.reference` in the user's
 * git configuration commits that option's placeholder as the subject.
 */
export interface RevertPlan {
  /** The exact line. */
  command: string;
  /** The full object name the daemon resolved. */
  commit: string;
  /** The commit's subject, for a confirmation that names more than a hash. */
  subject: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the revert
   * so a destination that moved is refused.
   */
  into: string;
  outcome: RevertOutcome;
}

/**
 * Which of the three trees a reset moves.
 *
 * A choice the client makes, not a fact the daemon reads — the same shape
 * PullStrategy has. Soft moves only HEAD; mixed moves HEAD and the index;
 * hard moves all three. Always pinned on the command so a bare `git reset`
 * cannot quietly mean mixed.
 */
export type ResetMode = 'soft' | 'mixed' | 'hard';

/**
 * What a reset would do, and the line it would run.
 *
 * Answered by the daemon before the reset. The mode travels with the request
 * and chooses the flag; the counts name what the confirmation has to say
 * about commits leaving the branch and about uncommitted work.
 */
export interface ResetPlan {
  /** The exact line. */
  command: string;
  /** The full object name the daemon resolved. */
  commit: string;
  /** The commit's subject, for a confirmation that names more than a hash. */
  subject: string;
  /**
   * The branch HEAD is on, as the daemon read it. Sent back with the reset
   * so a destination that moved is refused.
   */
  into: string;
  mode: ResetMode;
  /** Commits on HEAD past the target — what the branch would leave behind. */
  dropping: number;
  /**
   * Tracked paths that differ from HEAD or from the index — what a hard reset
   * puts back, and what only hard takes away. Untracked files are NOT counted:
   * `git reset --hard` leaves them where they are, so naming them under "this
   * will permanently discard" would be a warning about a loss that does not
   * happen.
   */
  dirty_files: number;
}

/** Which tip action Undo would reverse. */
export type UndoKind = 'commit' | 'amend' | 'checkout' | 'reset' | 'branch-delete';

/** What GET /undo answers when the tip can be undone. */
export interface UndoOffer {
  kind: UndoKind;
  subject: string;
  head: string;
  /** The branch a restore would make. Empty for every other kind. */
  branch: string;
}

export interface UndoStatus {
  available: boolean;
  offer?: UndoOffer;
}

/**
 * Reverse of the tip HEAD-reflog entry (commit, amend, or checkout).
 *
 * Leased by object names — see ADR 0031. Soft reset for commit, amend and
 * reset; switch or detach for checkout; `git branch` for a deletion the
 * daemon remembered (ADR 0032 — the reflog cannot see that one).
 */
export interface UndoPlan {
  command: string;
  kind: UndoKind;
  into: string;
  head: string;
  to: string;
  to_ref: string;
  /** The branch a restore would make. Empty for every other kind. */
  branch: string;
  detach: boolean;
  subject: string;
}

/**
 * Where a push would go, and the line it would run.
 *
 * Answered by the daemon before the push, because the destination is read off
 * the branch's upstream and the browser has no business working that out: a
 * branch called `main` that follows `origin/trunk` pushes to trunk, and a
 * refspec assembled here would eventually push to the wrong one.
 */
export interface PushPlan {
  /** The exact line, for the confirmation to show. */
  command: string;
  remote: string;
  /** The full ref on the other side: refs/heads/trunk. */
  ref: string;
  local_branch: string;
  /** True when the branch follows nothing yet, so this push publishes it. */
  publishing: boolean;
}

/**
 * What setting or unsetting a branch's upstream would run.
 *
 * Distinct from publishing: no push. `remote` and `upstream` are empty on an
 * unset plan.
 */
export interface UpstreamPlan {
  command: string;
  branch: string;
  remote?: string;
  upstream?: string;
}

/**
 * What pushing a tag would run, before it runs.
 *
 * Separate from PushPlan: a tag has no upstream and no publish-vs-push split,
 * and the destination is always refs/tags/… under a remote the user chose.
 */
export interface TagPushPlan {
  command: string;
  remote: string;
  name: string;
  /** Full ref: refs/tags/v1.0.0. */
  ref: string;
}

/**
 * What cloning would run, before it runs.
 *
 * The command is the confirmation's whole content — URL credentials stripped,
 * destination already checked against the root. Path is the resolved
 * destination the daemon will use.
 */
export interface ClonePlan {
  command: string;
  path: string;
}

/**
 * What making an empty repository would run.
 *
 * `branch` comes back filled even when the request left it empty: the first
 * branch's name is `init.defaultBranch` on the machine the daemon runs on, and
 * the browser cannot see that setting. The field on screen is filled from
 * this, so the name the user approves is the name the command shows.
 */
export interface InitPlan {
  command: string;
  path: string;
  branch: string;
}

export interface Repository {
  id: string;

  /**
   * Where it is on disk.
   *
   * The tab bar's only way of telling two repositories called "api" apart,
   * and the same string `/api/repos/discover` reports for every repository it
   * finds. See the note on repositoryView in internal/api/views.go.
   */
  path: string;
  name: string;

  /**
   * A repository with no work tree — a mirror, a `--bare` clone.
   *
   * There is no work tree to show for one and no file to stage, so the
   * screens that assume both have to ask. The daemon has always sent this; the
   * omission here was the type quietly disagreeing with the wire.
   */
  bare: boolean;

  opened_at: string;

  /**
   * Whether the daemon is following this repository's git directories.
   *
   * False is the value worth drawing. The watch can be refused — more ref
   * directories than it will follow, or none left on the machine — and a
   * repository that will never refresh again looks exactly like one where
   * nothing is happening. `watch_failure` carries the reason, in the operating
   * system's own words, so the message can say what to do about it.
   */
  watched: boolean;
  watch_failure?: string;
}

export type DiscoveredKind = 'top-level' | 'worktree' | 'submodule';

export interface DiscoveredRepository {
  path: string;
  name: string;
  bare: boolean;
  kind: DiscoveredKind;
}

/**
 * Why a scan found what it found: the directories it refused to look inside,
 * counted by the reason it refused.
 *
 * Every field is always sent, zero included, so nothing here has to tell
 * "none" apart from "not reported". Mirrors repo.DiscoverSkipped field for
 * field; the daemon is where the reasons are decided.
 */
export interface DiscoverSkipped {
  /** Could not be listed at all. */
  unreadable: number;
  /** More levels below the scanned directory than `depth` allows. */
  too_deep: number;
  /** Named node_modules, vendor, target or .cache. */
  ignored_name: number;
  /** Named with a leading dot. */
  dotted: number;
  /** A linked worktree, and the scan was not asked for those. */
  worktrees: number;
  /** A submodule checkout, and the scan was not asked for those. */
  submodules: number;
  /** Holds a .git that git then refused to call a repository. */
  not_a_repository: number;
}

/**
 * A repository the scan could not finish inspecting.
 *
 * It arrives on a successful response: the scan worked, and this is one
 * repository listed without its submodules. `git` is what git said, whole,
 * wherever git is what refused.
 */
export interface DiscoverFailure {
  path: string;
  message: string;
  git?: GitFailure;
}

export interface DiscoverResult {
  repos: DiscoveredRepository[];
  scanned_from: string;
  root: string;
  /** Levels below `scanned_from` this scan went. The daemon owns the default. */
  depth: number;
  /** The deepest a scan may be asked to go. The daemon owns the ceiling too. */
  depth_limit: number;
  skipped: DiscoverSkipped;
  submodule_failures: DiscoverFailure[];
}

/**
 * A git command the daemon actually ran.
 *
 * This type is what makes the project's promise verifiable: the user must
 * always be able to learn git by watching yagit work.
 */
export interface GitExecution {
  id: string;
  /** The line as you would type it in a terminal. */
  command: string;
  exit_code: number;
  duration_ms: number;
  stderr: string;
  started_at: string;
}

/** What the daemon returns when a git command fails. */
export interface GitFailure {
  command: string;
  args: string[];
  exit_code: number;
  stderr: string;
}

export interface ApiErrorPayload {
  error: {
    message: string;
    git?: GitFailure;
  };
}

/**
 * The two-letter status field git writes for a path, one character per side.
 * `.` is git's "nothing changed here", which the short format writes as a
 * space.
 */
export type StatusCode = '.' | 'M' | 'A' | 'D' | 'R' | 'C' | 'U' | 'T';

/**
 * The shape of a status record, which decides how its two codes are read. An
 * unmerged entry's codes are not index-and-work-tree at all: they are
 * us-and-them, and `conflict` is the sentence that says which.
 */
export type EntryKind = 'ordinary' | 'renamed' | 'copied' | 'unmerged' | 'untracked';

export interface SubmoduleState {
  commit_changed: boolean;
  modified: boolean;
  untracked: boolean;
}

/**
 * One path git reports as differing from HEAD, from the index, or from both.
 *
 * `staged` and `unstaged` are not mutually exclusive, and that is the point:
 * `git add` then edit again leaves a file in both lists at once. An interface
 * that showed one verb per file would have to pick which half to lie about.
 */
export interface FileStatus {
  path: string;
  old_path?: string;
  kind: EntryKind;

  index: StatusCode;
  work_tree: StatusCode;

  staged: boolean;
  unstaged: boolean;

  /** How the path is unmerged, in words. Empty when it is not. */
  conflict?: string;

  /** git's similarity percentage for a rename or a copy. */
  score?: number;

  submodule?: SubmoduleState;
}

/**
 * The unfinished thing a repository is in the middle of.
 *
 * git records these as files in the work tree's git directory and prints them
 * above the file list in a terminal. `git status --porcelain` says nothing
 * about them, so without this the panel shows seven conflicted files and no
 * word about the merge that produced them.
 */
export type Operation =
  | ''
  | 'merge'
  | 'rebase'
  | 'cherry-pick'
  | 'revert'
  | 'bisect'
  /** `git am`, applying a mailbox of patches. */
  | 'am';

/**
 * What can be done about an operation in progress.
 *
 * git's own three flags, and the whole vocabulary: there is no fourth thing to
 * do about a stopped rebase. Which of them apply to which operation is the
 * daemon's to say — `git merge --skip` does not exist — and it says so in the
 * plan rather than leaving the browser to keep a second copy of the table.
 */
export type OperationAction = 'abort' | 'continue' | 'skip';

/** What one instruction would run, asked before the confirmation opens. */
export interface OperationPlan {
  /** The exact line, for the confirmation to show. */
  command: string;

  /**
   * What the daemon found in progress, which is the fact the command was built
   * from. The banner may be up to two seconds stale; this is not.
   */
  operation: Operation;

  /**
   * Which instance of that operation — MERGE_HEAD, the rebase's onto, and so
   * on. Sent back on the run so an Abort aimed at one rebase cannot destroy
   * the next that started under the same kind.
   */
  identity: string;

  action: OperationAction;

  /**
   * Whether this throws work away, and so whether to ask first. Decided in the
   * daemon because it is a fact about git rather than about a screen.
   */
  destroys: boolean;
}

export interface RepositoryState {
  operation: Operation;
  /** Identifies this instance of the operation; see OperationPlan.identity. */
  identity?: string;
  /** The branch a rebase is replaying, short. Absent for everything else. */
  branch?: string;
  /** How far a rebase has got. Both absent, or both present. */
  step?: number;
  total?: number;

  /**
   * What can be done about this operation, in the order to draw it.
   *
   * The daemon's list, never one kept here: which instructions an operation
   * takes is a fact about git — `git merge --skip` does not exist — and a
   * second copy of that table in the browser is how a button comes to exist
   * for a command the daemon refuses. Absent when nothing is in progress.
   */
  actions?: OperationAction[];

  /**
   * Why one of those actions cannot be run on THIS repository, against the
   * action, and in the sentence to show.
   *
   * `actions` is about the kind of operation; this is about the one in front
   * of you — a rebase with a `squash` still in its plan is a rebase that takes
   * `--continue`, and continuing it here would commit a message nobody has
   * seen. The daemon refuses these too, with this same string, so the button's
   * explanation and the route's refusal cannot drift apart.
   */
  blocked?: Partial<Record<OperationAction, string>>;
}

/** Where HEAD is, how it stands against its upstream, and what differs. */
export interface WorkingDirectory {
  /** Empty when HEAD is detached, which `detached` says explicitly. */
  branch: string;
  detached: boolean;

  /** Empty on a branch whose first commit does not exist yet. */
  head_sha: string;
  unborn: boolean;

  upstream?: string;
  ahead: number;
  behind: number;

  /** What the work tree is in the middle of, if anything. */
  state: RepositoryState;

  files: FileStatus[];
}

/**
 * Which of a path's two diffs is wanted.
 *
 * They are two different questions with two different answers, and a file can
 * have both at once. `untracked` is the third: the whole content of a file git
 * has never seen, as one addition per line.
 */
export type DiffSide = 'staged' | 'unstaged' | 'untracked';

export type DiffLineKind = 'context' | 'added' | 'removed';

export interface DiffLine {
  kind: DiffLineKind;
  /** The line without its leading '+', '-' or ' '. */
  text: string;
  /**
   * The address a line selection uses: every body line of the file's diff,
   * numbered from zero across every hunk.
   */
  index: number;
  /** The line's number on each side, or 0 where it does not exist there. */
  old_line: number;
  new_line: number;
  /** git's "\ No newline at end of file", which belongs to this line. */
  no_newline: boolean;
  /**
   * How many characters of this line the daemon did not send, absent on every
   * ordinary line.
   *
   * The line cap counts lines and never their length, so a minified bundle
   * arrives as two lines of three megabytes and lays out tens of thousands of
   * visual rows. The daemon keeps the whole line — a patch is built from it —
   * and shortens only the answer.
   */
  truncated?: number;
}

export interface DiffHunk {
  old_start: number;
  old_lines: number;
  new_start: number;
  new_lines: number;
  /** The function or section name git puts after the second @@. */
  heading: string;
  lines: DiffLine[];
}

export interface FileDiff {
  /**
   * Fingerprint of the exact bytes git produced.
   *
   * Sent back with a line selection so the daemon can refuse one made against
   * a diff the file has since moved past. Without it, a patch built from stale
   * line numbers could still apply — somewhere.
   */
  id: string;

  path: string;
  /** Set only when the two sides have different names. */
  old_path?: string;

  /** git refused to show the content: there is nothing to take line by line. */
  binary: boolean;

  /** The file is absent from one side: a creation, a deletion. */
  added: boolean;
  removed: boolean;

  old_mode?: string;
  new_mode?: string;

  /**
   * Set when what this diff shows is a Git LFS pointer rather than the file.
   *
   * Absent for nearly every diff. A pointer is three short lines of metadata,
   * so a diff of one is perfectly readable and perfectly useless: it says an
   * OID changed where somebody expected to see their picture change. Naming it
   * is what lets the pane say so.
   */
  lfs?: LFSDiff;

  hunks: DiffHunk[];
}

/** What a committed LFS pointer says about the file it stands for. */
export interface LFSPointer {
  /** The object identifier, `sha256:<64 hex>` as the pointer file spells it. */
  oid: string;
  /** The real file's size in bytes — the number shown in place of a diff. */
  size: number;
}

/**
 * What the two sides of a diff point at, where either is a pointer.
 *
 * Both halves are optional and for the reason a diff has an `added` and a
 * `removed` flag: a file newly tracked by LFS has a pointer on the new side
 * and its real content on the old, and one being untracked is the reverse.
 */
export interface LFSDiff {
  old?: LFSPointer;
  new?: LFSPointer;
}

/**
 * What Git LFS can do here, and what it is doing.
 *
 * Two facts about two different things, which is why they arrive together:
 * whether git-lfs is installed is a property of the MACHINE, and which
 * patterns go through the filter is a property of a file in this work tree.
 * Neither answers the question on its own.
 */
export interface LFSSupport {
  /**
   * Whether `git lfs` answers. Everything that moves bytes needs it;
   * recognising a pointer in a diff does not, which is why a diff can be
   * labelled on a machine that has never had git-lfs installed.
   */
  installed: boolean;
  /** What git-lfs calls itself, empty when it is not installed. */
  version: string;
  /** The patterns the top-level .gitattributes routes through the filter. */
  patterns: string[];
}

/**
 * Part of one file, named by the diff it was chosen from.
 *
 * The daemon re-reads that diff and refuses the request if the fingerprint no
 * longer matches, so this is not advisory: it is the whole safety of staging
 * by line.
 */
export interface LineSelection {
  diff: string;
  indices: number[];
}

/**
 * What a discard would run, in the order it would run.
 *
 * Composed by the daemon rather than here, because the daemon is the only side
 * that knows: every path reaches git as a `:(literal)` pathspec — without which
 * a file named `*` turns "discard this one file" into `git clean --force -- *`
 * — and the split between `git restore` and `git clean` follows a status this
 * side never reads. A command composed in the browser is a promise about
 * somebody else's argument list.
 */
export interface DiscardPlan {
  commands: string[];
}

export interface CommitResult {
  sha: string;
}

/** Which file the prepared message came from, so the box can say whose it is. */
export type MessageSource = '' | 'merge' | 'squash' | 'head' | 'template';

/**
 * The message git would open an editor on.
 *
 * Not a suggestion, and the difference decides how the box treats it. This is
 * what git itself would put in front of the user — MERGE_MSG after a conflict,
 * the replaced commit's message under `--amend` — so it goes IN the box as
 * real, editable text. What yagit makes up from the file list is a
 * placeholder, and stays one until somebody accepts it.
 */
export interface PreparedMessage {
  text: string;
  source: MessageSource;

  /**
   * Whether `commit.gpgsign` is on, so the box can say so before the button is
   * pressed.
   *
   * Signing is the step that fails once everything else has succeeded — a
   * locked key, an agent that is not running, a smartcard nobody touched — and
   * a box that never mentioned it turns that into "the button did not work".
   *
   * Read, never written: whose key a commit carries stays the person's own
   * decision, made in git's configuration.
   */
  signing: boolean;

  /**
   * Why `signing` could not be answered, absent when it was.
   *
   * The question can fail on its own — `commit.gpgsign` spelled as something
   * git will not read as a boolean — and the daemon answers with the message
   * anyway rather than failing the request. During a stopped merge the message
   * is git's own MERGE_MSG and there is nowhere else on this screen to get it
   * back from; a decorative flag must not take it down. So the box shows the
   * text and says, underneath, that it could not tell.
   */
  signing_unreadable?: string;
}

/**
 * Which line ending a file uses on disk.
 *
 * It has to travel, because it cannot survive the trip: a text box normalises
 * every newline it is given to a bare LF. Without this a CRLF file edited here
 * would be saved back with every line ending changed — a diff touching the
 * whole file for a one-line fix.
 */
export type EOL = 'lf' | 'crlf';

/** A work-tree file, as the editing pane holds it. */
export interface WorkFile {
  path: string;
  /** The content, with every line ending normalised to LF. */
  text: string;
  /**
   * The exact bytes that were on disk, fingerprinted.
   *
   * Sent back with a save so the daemon can refuse one built on content the
   * file has since moved past — the same guard as a line selection's diff id,
   * for the same reason.
   */
  fingerprint: string;
  eol: EOL;
  /** The file used both endings, so any save rewrites lines nobody typed in. */
  mixed_eol: boolean;
  executable: boolean;
  bytes: number;
}

/** What a save answers with: the file as it now is, and the status that followed. */
export interface SaveResult {
  file: WorkFile;
  status: WorkingDirectory;
}

/**
 * Which version of a conflicted file to keep.
 *
 * git's own words and git's own switches. During a REBASE they mean the
 * opposite of what most people expect — "ours" is the branch being replayed
 * onto — so the notes beside the buttons follow the operation in progress
 * rather than renaming the buttons. See conflictSideNotes.
 */
export type ConflictSide = 'ours' | 'theirs';

/**
 * One commit, whole: what it says, who wrote it, and what it changed.
 *
 * The history list carries a subject and nothing more, which is right for a
 * list and not enough to read a commit by. This is the rest, asked for one
 * commit at a time — two hundred full messages and two hundred patches is not
 * a page anyone wants.
 */
export interface CommitDetail extends Commit {
  /** The message below the subject, blank line removed. Usually empty. */
  body: string;

  /**
   * The second identity every commit carries. It differs from the author
   * after a rebase, a cherry-pick, or a patch applied by somebody else —
   * which is exactly when a reader needs to know.
   */
  committer: string;
  committer_date: string;

  files: FileDiff[];

  /**
   * git's own verdict on the signature — the letter `%G?` prints. `N` for the
   * unsigned commit most commits are, `G` for a good signature, and six more
   * for the states between them; signature.ts is where each becomes words.
   *
   * The letter rather than a boolean, because "signed" is not a yes-or-no: a
   * good signature from an untrusted key, an expired key, and a signature git
   * had no key to check are three different answers and none is a forgery.
   */
  signature: string;

  /**
   * Who git says signed it, absent when nothing did.
   *
   * Not the author line — a commit signed by somebody other than its author is
   * exactly the case worth seeing, and it is the one a name read off the
   * author would hide.
   */
  signer?: string;

  /**
   * The diff below is a merge's against its FIRST parent, not the whole of
   * what the merge brought in. A merge has one answer per parent and git
   * prints none of them by default; showing the first is what every tool
   * does, and saying so is what keeps it from being a lie.
   */
  against_first_parent: boolean;
}

/**
 * One entry of the stash stack.
 *
 * The index and the object name are both identity, and they are not the same
 * kind of identity — which is the whole of what makes this family different
 * from every other operation in this interface. `index` is a POSITION:
 * stash@{0} is whatever is most recent, so it names a different stash after
 * anything is pushed or dropped. `sha` never moves.
 *
 * git takes the position and refuses the object name — `git stash drop` will
 * not accept a SHA — so the position is what runs, and every write sends both
 * so the daemon can refuse a position that has come to hold something else.
 */
export interface Stash {
  index: number;
  sha: string;
  /**
   * What the stash is called, with git's own "On main: " prefix taken off.
   * The branch below is that prefix, read apart so a list of rows does not all
   * begin with the same four words.
   */
  message: string;
  /** The branch it was made on, empty where it was made on a detached HEAD. */
  branch: string;
  date: string;
}

/**
 * What names one stash to the daemon: where it is, and what was there.
 *
 * Both halves, always, and that pairing is the whole safety of this family.
 * The position is what git takes and the object name is what the position
 * meant when the row was drawn, so the daemon can read the position again and
 * refuse one that has come to hold somebody else's work.
 *
 * A plan satisfies it as readily as a row does, which is the point: a
 * confirmation re-asked with a different mode is still about the same stash.
 */
export type StashHandle = Pick<Stash, 'index' | 'sha'>;

/** One stash and everything it holds. */
export interface StashDetail extends Stash {
  files: FileDiff[];
}

/**
 * What stashing the work tree would save.
 *
 * No command, unlike every other plan here, and that is deliberate: a stash
 * takes nothing away — the whole feature is getting the work back — so the
 * dialog is a form rather than a destructive confirmation. What it needs is
 * these two counts, because the command treats them apart.
 */
export interface StashPushPlan {
  /** The branch it would be filed under, empty on a detached HEAD. */
  branch: string;
  /** The question echoed back, so two answers in flight cannot be mixed. */
  include_untracked: boolean;
  /** Tracked paths that differ — what is saved with no flag at all. */
  tracked: number;
  /** Paths git does not track yet — saved only when the box is ticked. */
  untracked: number;
}

/**
 * Whether the entry survives being put back.
 *
 * Two words that are one git subcommand each: `apply` leaves the stash in the
 * stack, `pop` removes it. A choice the client makes rather than a fact the
 * daemon reads — the shape ResetMode has — so it is always pinned on the
 * command.
 */
export type StashApplyMode = 'apply' | 'pop';

/** What putting one stash back would run. */
export interface StashApplyPlan {
  /** The exact line. */
  command: string;
  index: number;
  sha: string;
  message: string;
  branch: string;
  mode: StashApplyMode;
  /** How many paths the stash holds. */
  files: number;
  /**
   * Tracked paths that currently differ from HEAD or from the index.
   *
   * Not a refusal — putting a stash back onto work in progress is ordinary,
   * and git merges the two — but it is the whole difference between an apply
   * that lands silently and one that stops on a conflict.
   */
  dirty_files: number;
}

/** What dropping one stash would run, and what goes with it. */
export interface StashDropPlan {
  command: string;
  index: number;
  sha: string;
  message: string;
  branch: string;
  /** How many paths go with it — the loss the confirmation names. */
  files: number;
}
