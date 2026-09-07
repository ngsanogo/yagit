import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import type { GitExecution } from '../api/types';

/**
 * The daemon's push channel, read once for the whole session.
 *
 * One EventSource, never one per repository: a browser opens at most six
 * connections to an origin, and a stream per repository would park all of them
 * and leave every ordinary request queued behind (ADR 0007). Each event names
 * the repository it concerns, so one stream is enough.
 *
 * EventSource cannot set a request header, so this authenticates by cookie —
 * the credential the token exchange already established. That is what makes
 * the cookie load-bearing rather than a convenience.
 *
 * Reconnection is EventSource's own, with the delay the daemon sends and
 * Last-Event-ID sent back automatically. Nothing here retries by hand.
 */

/** How much of the command log the interface keeps on screen. */
const KEPT_EXECUTIONS = 500;

export type StreamState = 'connecting' | 'live' | 'lost';

export interface EventStream {
  state: StreamState;
  /** The git commands that have run, newest last. */
  executions: GitExecution[];
}

interface RepositoryChanged {
  id: string;
}

/**
 * Reads an event's payload, saying so rather than throwing.
 *
 * An exception raised inside an EventSource listener does not reach any
 * boundary this application controls: the browser reports it to
 * `window.onerror` and the stream carries on as if nothing happened. That is
 * exactly the silent failure this project refuses, so the parse is guarded and
 * the console keeps the payload that could not be read.
 *
 * Nothing should ever land here — the daemon writes these bytes with
 * `json.Marshal` a moment earlier. Which is why it is worth a line in the
 * console rather than a shrug.
 */
function parseEvent<T>(data: string): T | undefined {
  try {
    return JSON.parse(data) as T;
  } catch (cause) {
    console.error('unreadable event from the daemon', data, cause);
    return undefined;
  }
}

export function useEvents(initialExecutions: GitExecution[]): EventStream {
  const queryClient = useQueryClient();
  const [state, setState] = useState<StreamState>('connecting');
  const [executions, setExecutions] = useState<GitExecution[]>([]);

  useEffect(() => {
    const source = new EventSource('/api/events');

    source.onopen = () => setState('live');

    // EventSource reports every failure the same way and reconnects on its
    // own. Saying "lost" rather than "failed" is the honest wording: it will
    // come back, and the interface has to show that it is not currently being
    // told about changes — an interface that has silently stopped refreshing
    // is worse than one that never refreshed.
    source.onerror = () => setState('lost');

    source.addEventListener('repository', (event: MessageEvent<string>) => {
      const changed = parseEvent<RepositoryChanged>(event.data);
      if (changed === undefined) {
        return;
      }

      // Everything about that repository is now suspect: its history, the
      // commit that is open, its refs, its working directory. Invalidating by
      // prefix says exactly that and needs no list of which queries exist.
      //
      // 'commits' and 'commit' are two prefixes, not one: a key is matched
      // element by element, so the page of history and the single commit
      // opened under it each need naming. The single commit is what carries
      // the branch names decorating it, and those move.
      void queryClient.invalidateQueries({ queryKey: ['commits', changed.id] });
      void queryClient.invalidateQueries({ queryKey: ['commit', changed.id] });
      void queryClient.invalidateQueries({ queryKey: ['refs', changed.id] });
      void queryClient.invalidateQueries({ queryKey: ['status', changed.id] });
      void queryClient.invalidateQueries({ queryKey: ['diff', changed.id] });
      // The undo offer is a reading of the HEAD reflog, so anything that moves
      // HEAD — in this window, in another one, or in a terminal — changes it.
      // Here rather than on a poll of its own: this stream is how the interface
      // learns that a repository moved, and a second mechanism beside it would
      // be two answers to one question.
      void queryClient.invalidateQueries({ queryKey: ['undo', changed.id] });
    });

    source.addEventListener('git', (event: MessageEvent<string>) => {
      const execution = parseEvent<GitExecution>(event.data);
      if (execution === undefined) {
        return;
      }
      setExecutions((current) => {
        // A reconnection replays what was missed, and a replay can overlap
        // with what is already held. The identifier is the daemon's own
        // sequence number, so dropping a repeat is exact.
        if (current.some((held) => held.id === execution.id)) {
          return current;
        }
        return [...current, execution].slice(-KEPT_EXECUTIONS);
      });
    });

    return () => source.close();
  }, [queryClient]);

  // The backlog first, then what arrived since. Both come from the same ring
  // in the daemon, so an entry can be in both; the identifier settles it.
  const merged = [...initialExecutions];
  const known = new Set(merged.map((execution) => execution.id));
  for (const execution of executions) {
    if (!known.has(execution.id)) {
      merged.push(execution);
    }
  }

  return { state, executions: merged.slice(-KEPT_EXECUTIONS) };
}
