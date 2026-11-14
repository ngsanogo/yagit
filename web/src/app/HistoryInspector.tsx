import type { HistoryScope, Operation } from '../api/types';
import { BlamePanel } from './Blame';
import { CommitDetails } from './CommitDetails';
import type { DialogSlot } from './dialogSlot';
import { FileHistoryPanel } from './FileHistory';
import type { HistoryOperations } from './historyOperations';
import type { HistoryProposals } from './historyProposals';
import type { Inspected } from './HistoryView';
import { LineHistoryPanel } from './LineHistory';
import { StashDetails } from './StashDetails';
import { checkOutRequestForCommit } from './useCheckOut';

/**
 * The height the panel under the list is given, in one place because all five
 * of them are the same slot and a disagreement between them would be a panel
 * that jumps when the kind of thing open in it changes.
 *
 * `flex-1` over whatever the list above leaves — not a fixed fraction. A
 * click opens this panel to READ a commit (or a stash, or a path's history),
 * so it is the main object once it is open: the list keeps a capped strip
 * tall enough to stay oriented, and this slot takes the rest. A floor keeps
 * a short window from collapsing the patch to nothing. `overflow-hidden`
 * keeps a tall patch's layout height from leaking past this slot into the
 * page.
 */
const INSPECT_PANEL = 'min-h-80 flex-1 basis-0 overflow-hidden';

/**
 * What is open under the history when it is not a commit: a stash, or one
 * path's history, blame or line history. Nothing when nothing of those is.
 */
export function InspectedPanel({
  repositoryId,
  inspected,
  onInspect,
  onClose,
}: {
  repositoryId: string;
  inspected: Inspected | undefined;
  onInspect: (what: Inspected) => void;
  onClose: () => void;
}) {
  return (
    <>
      {inspected?.kind === 'stash' && (
        <div className={INSPECT_PANEL}>
          <StashDetails
            // Keyed by the position, so choosing another stash starts from
            // nothing rather than showing the previous one's patch under the
            // new one's header while it loads.
            key={inspected.index}
            repositoryId={repositoryId}
            index={inspected.index}
            onClose={onClose}
          />
        </div>
      )}

      {inspected?.kind === 'file-history' && (
        <div className={INSPECT_PANEL}>
          <FileHistoryPanel
            key={`${inspected.path}@${inspected.revision}`}
            repositoryId={repositoryId}
            path={inspected.path}
            revision={inspected.revision}
            onClose={onClose}
            onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
          />
        </div>
      )}

      {inspected?.kind === 'blame' && (
        <div className={INSPECT_PANEL}>
          <BlamePanel
            key={`${inspected.path}@${inspected.revision}`}
            repositoryId={repositoryId}
            path={inspected.path}
            revision={inspected.revision}
            onClose={onClose}
            onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
            onLineHistory={(line) =>
              onInspect({
                kind: 'line-history',
                path: inspected.path,
                revision: inspected.revision,
                line,
              })
            }
          />
        </div>
      )}

      {inspected?.kind === 'line-history' && (
        <div className={INSPECT_PANEL}>
          <LineHistoryPanel
            key={`${inspected.path}:${inspected.line}@${inspected.revision}`}
            repositoryId={repositoryId}
            path={inspected.path}
            line={inspected.line}
            revision={inspected.revision}
            onClose={onClose}
            onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
          />
        </div>
      )}
    </>
  );
}

/**
 * The commit open under the history, and every action its pane offers.
 *
 * The pane is the same component wherever a commit is read; what is decided
 * here is what this screen lets it do — which operations a bare repository or
 * a detached HEAD refuses, and which of them is still being planned.
 */
export function InspectedCommit({
  repositoryId,
  sha,
  scope,
  refs,
  headSha,
  detached,
  canCheckOut,
  operation,
  ops,
  proposals,
  dialog,
  onInspect,
  onClose,
}: {
  repositoryId: string;
  sha: string;
  scope: HistoryScope;
  refs: readonly string[];
  headSha: string | undefined;
  detached: boolean;
  canCheckOut: boolean;
  /** What the work tree is in the middle of, empty when nothing. */
  operation: Operation;
  ops: HistoryOperations;
  proposals: HistoryProposals;
  dialog: DialogSlot['dialog'];
  onInspect: (what: Inspected) => void;
  onClose: () => void;
}) {
  const whileDetached = (what: string) => (detached ? `HEAD is detached, so ${what}` : undefined);

  return (
    <div className={INSPECT_PANEL}>
      <CommitDetails
        // Keyed by the commit, so choosing another one starts from
        // nothing rather than showing the previous commit's patch under
        // the new one's message while it loads.
        key={sha}
        repositoryId={repositoryId}
        sha={sha}
        scope={scope}
        refs={refs}
        onClose={onClose}
        onCheckOut={
          canCheckOut ? () => ops.checkOut.mutate(checkOutRequestForCommit(sha)) : undefined
        }
        checkingOut={ops.checkOut.isPending && ops.checkOut.variables.ref === sha}
        onCherryPick={canCheckOut ? () => proposals.proposeCherryPick(sha) : undefined}
        cherryPicking={ops.cherryPick.plan.isPending && ops.cherryPick.plan.variables === sha}
        cherryPickRefusal={whileDetached('there is no branch to cherry-pick onto')}
        onRevert={canCheckOut ? () => proposals.proposeRevert(sha) : undefined}
        reverting={ops.revert.plan.isPending && ops.revert.plan.variables === sha}
        revertRefusal={whileDetached('there is no branch to revert on')}
        onReset={canCheckOut ? () => proposals.proposeReset(sha) : undefined}
        resetting={
          ops.reset.plan.isPending &&
          ops.reset.plan.variables?.commit === sha &&
          dialog?.kind !== 'reset'
        }
        resetRefusal={whileDetached('there is no branch to reset')}
        onRewrite={canCheckOut ? () => proposals.proposeRewrite(sha) : undefined}
        rewriting={ops.rewrite.plan.isPending && ops.rewrite.plan.variables === sha}
        rewriteRefusal={whileDetached('there is no branch whose commits could be rewritten')}
        onStartBisect={canCheckOut ? () => proposals.proposeStartBisect(sha) : undefined}
        startingBisect={ops.bisect.plan.isPending && ops.bisect.plan.variables?.good?.[0] === sha}
        startBisectRefusal={
          operation !== ''
            ? `A ${operation} is already in progress.`
            : sha === headSha
              ? 'This is HEAD, the end that is broken. Pick an older commit where it still worked.'
              : undefined
        }
        onFileHistory={(path) => onInspect({ kind: 'file-history', path, revision: sha })}
        onBlame={(path) => onInspect({ kind: 'blame', path, revision: sha })}
      />
    </div>
  );
}
