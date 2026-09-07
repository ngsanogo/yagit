import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';

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
import { MoonGlyph, SunGlyph } from '../components/ThemeGlyphs';
import { useToast } from '../components/ToastHost';
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
  writeStoredActivePath,
  writeStoredPaths,
  writeStoredTheme,
  type Theme,
} from './sessionStore';
import { UndoActions } from './UndoActions';
import { useEvents, type StreamState } from './useEvents';
import { useWorkingDirectory } from './useWorkingDirectory';

/**
 * The application.
 *
 * One repository at a time, chosen by a tab, and one face of it at a time,
 * chosen by the switch beside them: its history, or its working directory.
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

  useEffect(() => {
    applyTheme(readStoredTheme());
  }, []);

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
      for (const path of paths) {
        try {
          await api.openRepository(path);
        } catch {
          // The repository may have moved or left the allowed root.
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
      writeStoredPaths(opened.map((repository) => repository.path));
    })();
  }, [repositories.data, repositories.isPending, queryClient]);

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
    const open = repositories.data ?? [];
    const active = open.find((repository) => repository.id === activeId);
    writeStoredActivePath(active?.path);
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
        <QueryErrorState title="The daemon is not answering" error={repositories.error} />
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
          }
        }}
      />

      {active === undefined ? (
        <Centered>
          <EmptyState
            title="No repository open"
            description="Pick a repository from the scan, clone one, or open one by path. The daemon only reads inside the root it was started with."
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

      {showingLog && (
        <div className="h-64 shrink-0 px-3 pb-3">
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
}: {
  repositories: Repository[];
  active: Repository | undefined;
  streamState: StreamState;
  showingLog: boolean;
  onToggleLog: () => void;
  onSelect: (id: string) => void;
  onOpened: (id: string) => void;
  onClosed: (id: string) => void;
}) {
  const [opening, setOpening] = useState(false);
  const [theme, setTheme] = useState<Theme>(() => readStoredTheme());
  const queryClient = useQueryClient();
  const toast = useToast();

  const toggleTheme = () => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    applyTheme(next);
    writeStoredTheme(next);
  };

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
    <header className="flex shrink-0 items-center gap-4 border-b border-line bg-surface pr-4">
      <h1 className="shrink-0 py-2 pl-4 text-sm font-semibold text-ink">yagit</h1>

      {repositories.length > 0 && active !== undefined && (
        <Tabs
          className="min-w-0 flex-1"
          activeId={active.id}
          onSelect={onSelect}
          onClose={(id) => close.mutate(id)}
          items={repositories.map((repository) => ({
            id: repository.id,
            label: repository.name,
            detail: repository.bare ? 'bare' : repository.path,
          }))}
        />
      )}

      <div className="ml-auto flex shrink-0 items-center gap-2">
        <UnwatchedIndicator repository={active} />
        <StreamIndicator state={streamState} />

        <Button size="sm" variant="ghost" onClick={onToggleLog} aria-pressed={showingLog}>
          Git log
        </Button>

        <Button
          size="sm"
          variant="ghost"
          onClick={toggleTheme}
          leading={theme === 'dark' ? <SunGlyph /> : <MoonGlyph />}
          aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        >
          {theme === 'dark' ? 'Light' : 'Dark'}
        </Button>

        {/* Reachable whatever is on screen. The tab bar holds many
            repositories, so a way to open one that exists only when none is
            open would cap the application at exactly one — which the tabs
            would then never show. */}
        <Button size="sm" onClick={() => setOpening(true)}>
          Open repository
        </Button>
      </div>

      <Dialog
        open={opening}
        onClose={() => setOpening(false)}
        title="Add a repository"
        description="Open one the daemon finds on disk, or clone a remote into the allowed root."
        className="max-w-xl"
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
                badge: changed > 0 ? <Badge tone="accent">{changed}</Badge> : undefined,
              },
            ]}
          />

          {status.data?.branch !== undefined && status.data.branch !== '' && (
            <span className="truncate font-mono text-xs text-ink-muted">
              on {status.data.branch}
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
            <Spinner label="Reading the working directory" />
          </Centered>
        ) : status.isError ? (
          <Centered>
            <QueryErrorState title="Could not read the working directory" error={status.error} />
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
