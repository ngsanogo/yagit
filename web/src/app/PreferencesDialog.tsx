import { useQuery } from '@tanstack/react-query';

import { Button } from '../components/Button';
import { Dialog } from '../components/Dialog';
import { Spinner } from '../components/Spinner';
import { errorDescription } from '../lib/errorDisplay';
import { IdentityForm, identityQuery } from './IdentityForm';

/**
 * Who commits are attributed to on this machine, and how pull.rebase is set.
 *
 * pull.rebase is shown and not offered: the Pull button already names its
 * three strategies, and a setting changed here would disagree with them.
 */
export function PreferencesDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const identity = useQuery({ ...identityQuery, enabled: open });

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Git preferences"
      description="Who commits are attributed to on this machine, and how pull.rebase is set."
      size="medium"
    >
      {identity.isPending && (
        <div className="grid place-items-center py-8">
          <Spinner label="Reading git preferences" />
        </div>
      )}

      {identity.isError && (
        <p className="text-sm text-danger">{errorDescription(identity.error)}</p>
      )}

      {identity.data !== undefined && (
        <div className="flex flex-col gap-4">
          {/* Remounted when the answer changes so the fields start from that
              reading rather than from an effect that copies into state. */}
          <IdentityForm
            key={`${identity.data.name}\0${identity.data.email}`}
            identity={identity.data}
            onSaved={onClose}
            secondary={
              <Button type="button" variant="ghost" onClick={onClose}>
                Cancel
              </Button>
            }
          />
          <p className="border-t border-line pt-3 text-sm text-ink-muted">
            <span className="font-medium text-ink">pull.rebase:</span>{' '}
            {identity.data.pull_rebase === ''
              ? 'unset — the Pull button names fast-forward, merge and rebase itself'
              : identity.data.pull_rebase}
          </p>
        </div>
      )}
    </Dialog>
  );
}
