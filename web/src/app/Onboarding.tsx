import { useQuery } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { Button } from '../components/Button';
import { Spinner } from '../components/Spinner';
import { errorDescription } from '../lib/errorDisplay';
import { IdentityForm, identityQuery } from './IdentityForm';
import { discoverQuery } from './OpenRepository';
import { writeOnboardingDone } from './sessionStore';

/**
 * First-run welcome when nothing is open yet: where the daemon may look, and
 * who commits will be attributed to.
 *
 * The add-repository form stays underneath. This only names the two facts a
 * first session usually lacks — the allowed root, and a committer identity —
 * so opening a repository is not the first time somebody meets them. The
 * identity is asked for with the same form as the preferences dialog, and
 * only when one is missing: a machine that already has one is told whose it
 * is and left alone.
 */
export function Onboarding({ onContinue }: { onContinue: () => void }) {
  const root = useQuery(discoverQuery({ includeWorktrees: false, includeSubmodules: false }));
  const identity = useQuery(identityQuery);

  const dismiss = () => {
    writeOnboardingDone();
    onContinue();
  };

  return (
    // The welcome itself is the empty state's heading above this card, so
    // the card holds the two facts and nothing that repeats it.
    <section
      aria-label="Before the first repository"
      className="flex w-full flex-col gap-4 rounded-lg border border-line bg-surface p-4 text-left shadow-panel"
    >
      <Hint title="Allowed root">
        {root.isPending ? (
          <Spinner label="Reading the allowed root" />
        ) : root.isError ? (
          <span className="text-danger">{errorDescription(root.error)}</span>
        ) : (
          <>
            The daemon only opens repositories under{' '}
            <code className="font-mono text-ink">{root.data.root}</code>. Clone and create
            destinations must land there too.
          </>
        )}
      </Hint>

      <Hint title="Committer identity">
        {identity.isPending ? (
          <Spinner label="Reading the identity" />
        ) : identity.isError ? (
          <span className="text-danger">{errorDescription(identity.error)}</span>
        ) : identity.data.name === '' || identity.data.email === '' ? (
          <div className="flex flex-col gap-3">
            <p>
              No <code className="font-mono">user.name</code> /{' '}
              <code className="font-mono">user.email</code> is set yet, and a commit needs both.
            </p>
            <IdentityForm identity={identity.data} />
          </div>
        ) : (
          <>
            Commits will be attributed to{' '}
            <span className="text-ink">
              {identity.data.name} &lt;{identity.data.email}&gt;
            </span>
            . Git preferences in the header change it.
          </>
        )}
      </Hint>

      <div className="flex justify-end">
        <Button size="sm" variant="secondary" onClick={dismiss}>
          Got it
        </Button>
      </div>
    </section>
  );
}

function Hint({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <h3 className="text-xs font-medium text-ink-muted">{title}</h3>
      <div className="text-sm text-ink-muted">{children}</div>
    </div>
  );
}
