import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';

import { api } from '../api/client';
import type { Repository } from '../api/types';
import { AddRepository } from './AddRepository';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { CommandLogPanel } from '../components/CommandLogPanel';
import { Dialog } from '../components/Dialog';
import { EmptyState } from '../components/EmptyState';
import { Centered, QueryErrorState } from '../components/PanelState';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { Tabs } from '../components/Tabs';
import { MoonGlyph, SunGlyph, SystemGlyph } from '../components/ThemeGlyphs';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenPath } from '../lib/path';
import type { Selection } from './ChangeList';
import { ChangesView } from './ChangesView';
import { HistoryView, type Inspected } from './HistoryView';
import { OperationBanner } from './OperationBanner';
import { RemoteActions } from './RemoteActions';
import {
  applyTheme,
  readStoredActivePath,
  readStoredPaths,
  readStoredTheme,
  resolveTheme,
  watchSystemTheme,
  writeStoredActivePath,
  writeStoredPaths,
  writeStoredTheme,
  type ThemeChoice,
} from './sessionStore';
import { UndoActions } from './UndoActions';
import { useEvents, type StreamState } from './useEvents';
import { useWorkingDirectory } from './useWorkingDirectory';

/**
 * The application.
 *
 * One repository at a time, chosen by a tab, and one face of it at a time,
 * chosen by the switch beside them: its history, or its work tree.
 * Navigation depth is capped at two and those two spend it, which is why there
 * is no tree and no breadcrumb.
 *
 * Two faces rather than three panes side by side. The graph is the main object
 * of one and the diff is the main object of the other, and a screen with two
 * main objects has none. What crosses between them is the count on the switch:
 * uncommitted work is never invisible, whichever face is showing.
 *
 * Open repositories and UI preferences are restored from session storage on
 * load; the daemon still holds them in memory only, but paths let the
 * workbench re-open what was here before a reload.
 */
export function Workbench() {
  const [activeId, setActiveId] = useState<string>();
  const [showingLog, setShowingLog] = useState(false);
  const queryClient = useQueryClient();
  /** False until the first restore pass finishes — including "nothing to restore". */
  const sessionReady = useRef(false);
  const restoredActive = useRef(false);
  const restoring = useRef(false);
  const toast = useToast();
  const theme = useThemeChoice();

  // The commands that ran before this page was loaded. What happens next
  // arrives on the stream, which is opened once for the whole session — one
  // EventSource however many repositories are open (ADR 0007).
  const backlog = useQuery({ queryKey: ['command-log'], queryFn: api.log });
  const events = useEvents(backlog.data ?? []);

  const repositories = useQuery({
    queryKey: ['repositories'],
    queryFn: api.listRepositories,
  });

  // Not gated on sessionReady: `./do up --restart` empties the daemon's
  // registry while the tab stays open, and refetch-on-focus is what tells the
  // interface. A one-shot restore would leave the user looking at "No
  // repository open" with three tabs they have to find and type again.
  //
  // It cannot loop: what is written back below is what actually opened, so a
  // path that no longer exists drops out of the store and the next pass has
  // nothing left to try.
  useEffect(() => {
    if (restoring.current || repositories.isPending) {
      return;
    }
    const open = repositories.data ?? [];
    if (open.length > 0) {
      sessionReady.current = true;
      return;
    }
    const paths = readStoredPaths();
    if (paths.length === 0) {
      sessionReady.current = true;
      return;
    }
    restoring.current = true;
    void (async () => {
      const refused: string[] = [];
      for (const path of paths) {
        try {
          await api.openRepository(path);
        } catch {
          // Collected rather than rethrown: one repository that has moved must
          // not stop the rest of the session coming back, and the loop has
          // nowhere to report from anyway. Reported once below, when the pass
          // is over and the count is known.
          refused.push(path);
        }
      }
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
      const opened = await queryClient.fetchQuery({
        queryKey: ['repositories'],
        queryFn: api.listRepositories,
      });
      const storedPath = readStoredActivePath();
      if (storedPath !== undefined) {
        const match = opened.find((repository) => repository.path === storedPath);
        if (match !== undefined) {
          setActiveId(match.id);
        }
      }
      restoredActive.current = true;
      restoring.current = false;
      sessionReady.current = true;
      // What actually opened — paths that failed are dropped from the store.
      // That is what stops this effect retrying them on every refetch-on-focus
      // for the rest of the session, and it is why the loss is announced: a
      // tab set that comes back one short, with nothing said, is the user
      // wondering whether they ever had it open.
      writeStoredPaths(opened.map((repository) => repository.path));
      if (refused.length > 0) {
        const noun = refused.length === 1 ? 'repository' : 'repositories';
        toast.push({
          tone: 'warning',
          title: `${refused.length} remembered ${noun} could not be reopened`,
          // Named, one per line. "Some could not be reopened" leaves the reader
          // counting tabs against a memory of what was there, which is the
          // work this toast exists to save them.
          detail: (
            <div className="flex flex-col gap-1">
              <p>
                Moved, deleted, or outside the allowed root, and no longer remembered. Add a
                repository by path to bring one back.
              </p>
              <ul className="flex flex-col gap-0.5">
                {refused.map((path) => (
                  <li key={path}>{path}</li>
                ))}
              </ul>
            </div>
          ),
        });
      }
    })();
  }, [repositories.data, repositories.isPending, queryClient, toast]);

  useEffect(() => {
    if (!sessionReady.current || restoring.current || repositories.isPending) {
      return;
    }
    const open = repositories.data ?? [];
    if (open.length === 0 && readStoredPaths().length > 0) {
      // The daemon holds nothing while the store still remembers something.
      // That is a restarted daemon, not a closed tab — closing writes the
      // store itself, at the gesture — and the restore effect above is about
      // to reopen them. Writing an empty list here is what lost the tabs
      // twice: once from the screen, once from the store, with nothing left
      // for a reload to recover.
      return;
    }
    writeStoredPaths(open.map((repository) => repository.path));
  }, [repositories.data, repositories.isPending]);

  useEffect(() => {
    if (restoredActive.current) {
      return;
    }
    const open = repositories.data ?? [];
    if (open.length === 0) {
      return;
    }
    restoredActive.current = true;
    const storedPath = readStoredActivePath();
    if (storedPath === undefined) {
      return;
    }
    const match = open.find((repository) => repository.path === storedPath);
    if (match !== undefined) {
      // Deferred so restoring the tab does not synchronously re-render inside
      // the effect that runs when the repository list first answers.
      queueMicrotask(() => setActiveId(match.id));
    }
  }, [repositories.data]);

  useEffect(() => {
    // Two guards, one bug — and it is the bug that made phase 12's promise
    // half true.
    //
    // This effect runs on the first commit of every load, when the list has
    // not answered, nothing is selected, and the restore above has not had a
    // chance to read the store. It used to mirror that state faithfully —
    // `writeStoredActivePath(undefined)`, which REMOVES the key — so the
    // remembered repository was deleted before either restore path went
    // looking for it, and every reload landed on whichever repository the
    // daemon happened to list first. The tabs came back; the user did not come
    // back with them.
    //
    // So: nothing is written until the restore has had its turn, and
    // "nothing is selected" is never written at all. Forgetting is a gesture,
    // and it is done where the gesture is — in onClosed, below — which is the
    // same distinction the paths mirror above draws for the same reason.
    if (!restoredActive.current) {
      return;
    }
    const open = repositories.data ?? [];
    const active = open.find((repository) => repository.id === activeId);
    if (active === undefined) {
      return;
    }
    writeStoredActivePath(active.path);
  }, [activeId, repositories.data]);

  if (repositories.isPending) {
    return (
      <Centered>
        <Spinner label="Loading repositories" />
      </Centered>
    );
  }

  if (repositories.isError) {
    return (
      <Centered>
        {/* An EmptyState rather than the QueryErrorState every panel uses, and
            this is the one screen that earns the exception. A panel's failure
            is reported inside a workbench that still has a header, tabs and
            every other panel; this one returns before all of them, so what is
            on screen is these few lines and nothing else. A title, the same
            sentence again underneath it and no control was a dead end — the
            far rarer render crash next door offers two buttons.

            Not a reload: a page reloaded against a daemon that is down lands
            on the token door, which replaces a diagnosable message with a form
            nobody can fill in. Asking the same question again is the gesture
            that matches the cause, because the cause is usually a daemon
            somebody is in the middle of restarting. */}
        <EmptyState
          title="The daemon is not answering"
          description="Nothing is lost — the repositories are on disk. Start the daemon with ./do up, then try again."
          detail={errorDescription(repositories.error)}
          action={
            <Button
              variant="primary"
              loading={repositories.isFetching}
              onClick={() => void repositories.refetch()}
            >
              Try again
            </Button>
          }
        />
      </Centered>
    );
  }

  const open = repositories.data;
  // The tab a repository was selected into may be gone — the daemon restarted,
  // or the repository was replaced underneath. Falling back to the first keeps
  // the screen showing something real rather than an empty pane whose cause is
  // invisible.
  const active = open.find((repository) => repository.id === activeId) ?? open[0];

  return (
    <div className="flex h-dvh flex-col bg-canvas">
      <Header
        repositories={open}
        active={active}
        streamState={events.state}
        showingLog={showingLog}
        onToggleLog={() => setShowingLog((current) => !current)}
        onSelect={setActiveId}
        onOpened={setActiveId}
        // Closing the active tab has to let go of it, or the workbench keeps
        // asking for a repository the daemon has forgotten.
        onClosed={(closed) => {
          setActiveId((current) => (current === closed ? undefined : current));
          // Forgotten here, at the gesture, rather than by the effect that
          // mirrors the daemon's list. The two look identical from that
          // effect — the daemon holds nothing either way — and treating a
          // restarted daemon as a closed tab is what used to wipe the store.
          const path = open.find((repository) => repository.id === closed)?.path;
          if (path !== undefined) {
            writeStoredPaths(readStoredPaths().filter((remembered) => remembered !== path));
            // And the same for which one was on screen, for the same reason:
            // a store still naming a repository nobody has open sends the next
            // load looking for a tab that will not be there.
            if (readStoredActivePath() === path) {
              writeStoredActivePath(undefined);
            }
          }
        }}
        theme={theme.choice}
        onCycleTheme={theme.cycle}
      />

      {active === undefined ? (
        <Centered>
          <EmptyState
            title="No repository open"
            description="Pick a repository from the scan, clone one, or open one by path. The daemon only reads inside the allowed root it was started with."
            action={<AddRepository />}
          />
        </Centered>
      ) : (
        <RepositoryView
          // A tab switch remounts this, deliberately. The selected sha lives
          // inside it, and a sha means nothing in another repository: two
          // checkouts of the same work share commits, so a selection carried
          // across lights up a row nobody clicked, and one carried into a
          // history that lacks it is held against nothing. The key is what
          // makes the selection be created and destroyed with the view;
          // clearing it from an effect instead would paint the wrong row once
          // before correcting itself.
          //
          // The scroll offset goes with it, which is the point rather than the
          // cost: an offset measured in rows of one history is meaningless in
          // another, and it used to survive the switch too. Nothing is
          // refetched. Every query below is keyed by repository id, so the
          // switch had already dropped one set and subscribed the other; the
          // cache answers the remount and no spinner appears.
          key={active.id}
          repository={active}
        />
      )}

      {/* A share of the window with a ceiling, not a fixed band. At the size
          the design is judged at this is the 256px it always was; below it, a
          drawer that kept every pixel took them from the panels above, and the
          history reached a height with no room for one commit row. Opening the
          log to find out why an operation failed should not be a trade against
          the graph the operation was run from. */}
      {showingLog && (
        <div className="h-2/5 max-h-64 min-h-24 shrink px-3 pb-3">
          <CommandLogPanel executions={events.executions} className="h-full" />
        </div>
      )}
    </div>
  );
}

function Header({
  repositories,
  active,
  streamState,
  showingLog,
  onToggleLog,
  onSelect,
  onOpened,
  onClosed,
  theme,
  onCycleTheme,
}: {
  repositories: Repository[];
  active: Repository | undefined;
  streamState: StreamState;
  showingLog: boolean;
  onToggleLog: () => void;
  onSelect: (id: string) => void;
  onOpened: (id: string) => void;
  onClosed: (id: string) => void;
  theme: ThemeChoice;
  onCycleTheme: () => void;
}) {
  const [opening, setOpening] = useState(false);
  const queryClient = useQueryClient();
  const toast = useToast();

  // Closing is what makes a replaced repository recoverable. The daemon
  // refuses one that was deleted and re-cloned — correctly — and until now
  // there was no way back except restarting it.
  const close = useMutation({
    mutationFn: (id: string) => api.closeRepository(id),
    onSuccess: async (_result, id) => {
      onClosed(id);
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
    },
    onError: (error, id) => {
      const repository = repositories.find((item) => item.id === id);
      toast.push({
        tone: 'danger',
        title: repository ? `Could not close ${repository.name}` : 'Could not close repository',
        detail: error.message,
      });
    },
  });

  return (
    <header
      // Inset three, like the panel columns below it. The header is the one
      // band that spans the window, so its edges are read against every panel
      // edge under them, and four here made a step nobody chose.
      className="flex shrink-0 items-center gap-4 border-b border-line bg-surface pr-3"
    >
      <h1 className="shrink-0 py-2 pl-3 text-sm font-semibold text-ink">yagit</h1>

      {repositories.length > 0 && active !== undefined && (
        <Tabs
          className="min-w-0 flex-1"
          activeId={active.id}
          onSelect={onSelect}
          onClose={(id) => close.mutate(id)}
          // The path, shortened from the FRONT. It is here to tell two
          // checkouts called "api" apart, and those two agree on everything
          // except the directory above them — so the tail is the half worth
          // keeping, and the CSS truncate that used to do this cut exactly
          // that half. The whole of it is on the tab's title for the reader
          // who needs the rest.
          //
          // On the title whatever the line says, bare repositories included.
          // Theirs reads "bare" rather than a path, so two bare checkouts
          // called "api" are the same two strings on screen with nothing under
          // them — the hover is the only way back to which is which.
          items={repositories.map((repository) => ({
            id: repository.id,
            label: repository.name,
            detail: repository.bare ? 'bare' : shortenPath(repository.path),
            detailInFull: repository.path,
          }))}
        />
      )}

      <div className="ml-auto flex shrink-0 items-center gap-2">
        <UnwatchedIndicator repository={active} />
        <StreamIndicator state={streamState} />

        {/* "Command log", not "Git log". `git log` prints commit history, and
            the commit history is the screen this button is sitting on top of —
            so the one name that already means something in git was pointing at
            the wrong panel. Everything else in the project already says
            command log: the component, the architecture notes, ADR 0030.

            Drawn as pressed as well as announced as pressed. The state went to
            assistive technology and to nobody else, so a reader who opened the
            log, scrolled, and looked back at the header had no way to tell
            whether it was open without going to look for it. */}
        <Button
          size="sm"
          variant="ghost"
          onClick={onToggleLog}
          aria-pressed={showingLog}
          className={showingLog ? 'bg-selected text-ink' : undefined}
        >
          Command log
        </Button>

        <ThemeButton choice={theme} onCycle={onCycleTheme} />

        {/* Reachable whatever is on screen. The tab bar holds many
            repositories, so a way to open one that exists only when none is
            open would cap the application at exactly one — which the tabs
            would then never show.

            "Add", because behind it are three ways to get a repository —
            open, clone, create — and naming it after one of them hid the other
            two from anyone who already had a repository open. It agrees with
            the dialog it opens, which is the point.

            And the ellipsis, which is a rule rather than a flourish: "…" says
            the press asks a question before anything happens, and never that
            something is working — that is what a spinner is for. So it
            belongs on every label that opens a dialog and on none that act on
            the click, which is why Fetch, Pull and Check out stay bare. */}
        <Button size="sm" onClick={() => setOpening(true)}>
          Add repository…
        </Button>
      </div>

      <Dialog
        open={opening}
        onClose={() => setOpening(false)}
        title="Add a repository"
        description="Open one the daemon finds on disk, or clone a remote into the allowed root."
        size="medium"
      >
        {/* Mounted only while the dialog is open. A native <dialog> keeps its
            children in the document once closed, so a refused open would still
            be sitting on its row the next time the dialog appeared —
            explaining a request nobody in that session had made. */}
        {opening && (
          <AddRepository
            onOpened={(id) => {
              onOpened(id);
              setOpening(false);
            }}
          />
        )}
      </Dialog>
    </header>
  );
}

/**
 * The three states of the theme, in the order the control walks them.
 *
 * It starts at `system`, which is where a reader who has never touched it
 * already is, and the first press is the one that used to be the only press:
 * to light. From there, dark; from there, back to letting the machine decide.
 */
const THEME_CYCLE: Record<ThemeChoice, ThemeChoice> = {
  system: 'light',
  light: 'dark',
  dark: 'system',
};

const THEME_NAME: Record<ThemeChoice, string> = {
  system: 'System',
  light: 'Light',
  dark: 'Dark',
};

/**
 * The theme, chosen once and then kept true.
 *
 * A layout effect rather than an effect, and that is not a preference: React
 * runs effects after the browser has painted, so a reader whose theme is light
 * got one dark frame on every single load. The attribute has to be on the
 * element before that paint. The frame before React mounts at all is still
 * dark — that one belongs to index.html, which is where a fix for it would go.
 */
function useThemeChoice() {
  const [choice, setChoice] = useState<ThemeChoice>(() => readStoredTheme());

  useLayoutEffect(() => {
    applyTheme(resolveTheme(choice));
    // Subscribed whatever the choice is. A machine that changes its mind while
    // yagit is open is the whole reason "system" is a state rather than a
    // one-time read, and a listener that fires while the choice is explicit
    // re-applies the theme that was already there.
    return watchSystemTheme(() => applyTheme(resolveTheme(choice)));
  }, [choice]);

  const cycle = () => {
    const next = THEME_CYCLE[choice];
    setChoice(next);
    writeStoredTheme(next);
  };

  return { choice, cycle };
}

/**
 * The control that walks the three.
 *
 * It names the theme in force rather than the one a press would bring, which
 * is the change three states forced: "Light" on a button you press to GET
 * light and "Light" on a button that says you HAVE light are the same word,
 * and with a third state on the ring there is no reading of the first that
 * says whether the machine is being followed. What the press does is in the
 * label a screen reader hears and in the one a pointer hovers.
 */
function ThemeButton({ choice, onCycle }: { choice: ThemeChoice; onCycle: () => void }) {
  const description = `Theme: ${THEME_NAME[choice]}. Change to ${THEME_NAME[THEME_CYCLE[choice]]}.`;

  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={onCycle}
      leading={
        choice === 'system' ? <SystemGlyph /> : choice === 'light' ? <SunGlyph /> : <MoonGlyph />
      }
      aria-label={description}
      title={description}
    >
      {THEME_NAME[choice]}
    </Button>
  );
}

/**
 * Whether the daemon is still telling this page about changes.
 *
 * Silent while it is live, because a permanent green light is a light nobody
 * reads. It appears when the stream is gone, and it has to: an interface that
 * has quietly stopped refreshing is worse than one that never refreshed, and
 * every screen behind this header is showing a repository somebody else may be
 * changing.
 */
/**
 * Says when the repository on screen has stopped refreshing on its own.
 *
 * Beside the stream indicator because it is the same class of problem and the
 * worse half of it: a lost stream comes back, and a watch that was refused does
 * not. Without this, a repository the daemon could not follow looks exactly
 * like one where nothing is happening — a commit made in a terminal changes
 * nothing on screen, and nothing on screen says why.
 *
 * The daemon's own sentence is the title rather than a paraphrase: "more than
 * 512 ref directories" tells somebody what to do and "could not watch" does
 * not.
 */
function UnwatchedIndicator({ repository }: { repository: Repository | undefined }) {
  if (repository === undefined || repository.watched) {
    return null;
  }
  return (
    <Badge tone="warning" title={repository.watch_failure}>
      Not refreshing on its own — reopen to retry
    </Badge>
  );
}

function StreamIndicator({ state }: { state: StreamState }) {
  if (state === 'live') {
    return null;
  }
  return (
    <Badge tone={state === 'lost' ? 'warning' : 'neutral'}>
      {state === 'lost' ? 'Reconnecting — changes may be stale' : 'Connecting'}
    </Badge>
  );
}

/**
 * How many files differ, on the switch that leads to them.
 *
 * The number is drawn and the noun is not, and that split is deliberate: two
 * characters is all the room a badge on a segment has, while a radio group
 * builds its accessible name out of its own contents — so what a screen reader
 * was given was "Changes 2", a number with no unit. Two files, two commits,
 * two conflicts? The word costs nothing where it is put here and would cost
 * the switch its shape anywhere else.
 */
function ChangedCount({ count }: { count: number }) {
  return (
    <>
      <Badge tone="accent">{count}</Badge>
      <span className="sr-only">{count === 1 ? 'file changed' : 'files changed'}</span>
    </>
  );
}

type View = 'history' | 'changes';

function RepositoryView({ repository }: { repository: Repository }) {
  const [view, setView] = useState<View>('history');
  const [inspected, setInspected] = useState<Inspected>();

  // The draft and the chosen file belong to the repository, not to the view
  // that shows them. Switching to the history unmounts the changes view, and
  // that round trip — write half a message, read what the last commit said,
  // come back — is the one this switch exists to invite.
  const [draft, setDraft] = useState<string>();
  const [selectedFile, setSelectedFile] = useState<Selection>();

  // A bare repository has no work tree, so there is nothing to stage and the
  // daemon refuses the question. Not asking it is the difference between a
  // screen that offers what applies and one that shows an error nobody caused.
  const status = useWorkingDirectory(repository.id, !repository.bare);
  const changed = status.data?.files.length ?? 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Above the switch, and outside both views, because it is true of the
          repository rather than of either face of it. Somebody reading the
          history during a stopped rebase needs to know it is stopped just as
          much as somebody staring at the conflicted files. */}
      {status.data !== undefined && (
        <OperationBanner repositoryId={repository.id} status={status.data} />
      )}

      {!repository.bare && (
        <div className="flex shrink-0 items-center gap-3 px-3 pt-3">
          <SegmentedControl
            label="What to show of this repository"
            value={view}
            onChange={setView}
            segments={[
              { value: 'history', label: 'History' },
              {
                value: 'changes',
                label: 'Changes',
                // The count travels with the switch rather than living inside
                // the view it names, which is the whole reason it is here:
                // uncommitted work has to be visible from the history too.
                badge: changed > 0 ? <ChangedCount count={changed} /> : undefined,
              },
            ]}
          />

          {/* Which branch you are on is the first question a git client is
              opened to answer, and it was the quietest string on the screen —
              muted, unweighted, indistinguishable from the ghost labels either
              side of it. The name now wears the colour the sidebar already
              spends on HEAD, so the toolbar and the reference list say the
              same fact in the same voice. The preposition stays muted: it is
              grammar, not the answer. */}
          {status.data?.branch !== undefined && status.data.branch !== '' && (
            <span className="truncate font-mono text-xs text-ink-muted" title={status.data.branch}>
              on <span className="font-semibold text-ref-head">{status.data.branch}</span>
            </span>
          )}
          {status.data?.detached === true && <Badge tone="warning">detached HEAD</Badge>}

          {/* Beside the branch, because they are the operations that move it
              against the copy on another machine — and outside both views, for
              the reason the banner above is: fetching is worth offering to
              somebody reading the history just as much as to somebody staging
              files. */}
          <div className="ml-auto flex items-center gap-2">
            <UndoActions repositoryId={repository.id} />
            <RemoteActions repository={repository} status={status.data} />
          </div>
        </div>
      )}

      {view === 'changes' && !repository.bare ? (
        status.isPending ? (
          <Centered>
            <Spinner label="Reading the work tree" />
          </Centered>
        ) : status.isError ? (
          <Centered>
            <QueryErrorState
              title="Could not read the work tree"
              error={status.error}
              retry={status}
            />
          </Centered>
        ) : (
          <ChangesView
            repository={repository}
            status={status.data}
            selected={selectedFile}
            onSelect={setSelectedFile}
            draft={draft}
            onDraftChange={setDraft}
          />
        )
      ) : (
        <HistoryView
          repository={repository}
          inspected={inspected}
          onInspect={setInspected}
          onClearSelection={() => setInspected(undefined)}
          // What the work tree holds, read once above and passed down: the
          // stash panel offers to save it, and a button that offered to save
          // nothing would be a button that only ever produces a refusal.
          changed={changed}
        />
      )}
    </div>
  );
}
