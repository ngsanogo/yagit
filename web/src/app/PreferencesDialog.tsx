import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { api } from '../api/client';
import type { GitIdentity } from '../api/types';
import { Button } from '../components/Button';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { Spinner } from '../components/Spinner';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/**
 * Edit user.name and user.email, and show pull.rebase without changing it.
 */
export function PreferencesDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const identity = useQuery({
    queryKey: ['prefs', 'identity'],
    queryFn: api.identity,
    enabled: open,
  });

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

      {/* Remounted when the answer arrives so the fields start from that
          reading rather than from an effect that copies into state. */}
      {identity.data !== undefined && (
        <IdentityForm
          key={`${identity.data.name}\0${identity.data.email}`}
          identity={identity.data}
          onClose={onClose}
        />
      )}
    </Dialog>
  );
}

function IdentityForm({ identity, onClose }: { identity: GitIdentity; onClose: () => void }) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(identity.name);
  const [email, setEmail] = useState(identity.email);

  const save = useMutation({
    mutationFn: () => api.setIdentity(name, email),
    onSuccess: (next) => {
      queryClient.setQueryData(['prefs', 'identity'], next);
      toast.push({ tone: 'success', title: 'Committer identity saved' });
      onClose();
    },
    onError: (error: Error) => {
      toast.push({
        tone: 'danger',
        title: 'Could not save the identity',
        detail: errorDescription(error),
      });
    },
  });

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };

  const pullLabel =
    identity.pull_rebase === ''
      ? 'unset — the Pull button still names fast-forward, merge and rebase itself'
      : identity.pull_rebase;

  return (
    <form className="flex flex-col gap-4" onSubmit={onSubmit}>
      <Field
        label="user.name"
        value={name}
        onChange={(event) => setName(event.target.value)}
        autoComplete="name"
        required
      />
      <Field
        label="user.email"
        type="email"
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        autoComplete="email"
        required
      />
      <p className="text-sm text-ink-muted">
        <span className="font-medium text-ink">pull.rebase:</span> {pullLabel}
      </p>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" variant="primary" loading={save.isPending}>
          Save
        </Button>
      </div>
    </form>
  );
}
