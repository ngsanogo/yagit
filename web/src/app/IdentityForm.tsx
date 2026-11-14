import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent, type ReactNode } from 'react';

import { api } from '../api/client';
import type { GitIdentity } from '../api/types';
import { Button } from '../components/Button';
import { Field } from '../components/Field';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/** The query both screens read the identity through, so a save updates both. */
export const identityQuery = {
  queryKey: ['prefs', 'identity'],
  queryFn: api.identity,
} as const;

/**
 * user.name and user.email, edited where git keeps them for this machine.
 *
 * One form for every screen that asks, so the field names, the refusal and
 * the sentence about where the values go cannot drift apart between them.
 * The fields start from the reading they are given: a caller that wants them
 * to follow a newer one remounts the form with a key.
 */
export function IdentityForm({
  identity,
  onSaved,
  secondary,
}: {
  identity: GitIdentity;
  onSaved?: () => void;
  /** A button beside Save, such as Cancel in a dialog. */
  secondary?: ReactNode;
}) {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState(identity.name);
  const [email, setEmail] = useState(identity.email);

  const save = useMutation({
    mutationFn: () => api.setIdentity(name, email),
    onSuccess: (next) => {
      queryClient.setQueryData(identityQuery.queryKey, next);
      toast.push({ tone: 'success', title: 'Committer identity saved' });
      onSaved?.();
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

  return (
    <form className="flex flex-col gap-3" onSubmit={onSubmit}>
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
      {/* Where the values go, said before the button rather than after it:
          this writes the configuration every repository on the machine
          falls back to, and a repository that sets its own keeps it. */}
      <p className="text-2xs text-ink-subtle">
        Saved with <code className="font-mono">git config --global</code>. A repository with its own{' '}
        <code className="font-mono">user.name</code> or{' '}
        <code className="font-mono">user.email</code> keeps it.
      </p>
      <div className="flex justify-end gap-2">
        {secondary}
        <Button type="submit" variant="primary" loading={save.isPending}>
          Save identity
        </Button>
      </div>
    </form>
  );
}
