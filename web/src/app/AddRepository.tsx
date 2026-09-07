import { useState } from 'react';

import { SegmentedControl, type Segment } from '../components/SegmentedControl';
import { CloneRepository } from './CloneRepository';
import { InitRepository } from './InitRepository';
import { OpenRepository } from './OpenRepository';

type Mode = 'open' | 'clone' | 'init';

const MODES: readonly Segment<Mode>[] = [
  { value: 'open', label: 'Open' },
  { value: 'clone', label: 'Clone' },
  { value: 'init', label: 'Create' },
];

/**
 * The three ways a repository enters the workbench.
 *
 * Open finds one already on disk; clone copies one onto disk first; create
 * makes an empty one. They share a dialog because all three answer "get a
 * repository onto this screen", and a second entry point would hide the ones
 * people need less often.
 */
export function AddRepository({ onOpened }: { onOpened?: (id: string) => void }) {
  const [mode, setMode] = useState<Mode>('open');

  return (
    <div className="flex flex-col gap-4">
      <SegmentedControl
        label="How to add a repository"
        segments={MODES}
        value={mode}
        onChange={setMode}
      />
      {mode === 'open' && <OpenRepository onOpened={onOpened} />}
      {mode === 'clone' && <CloneRepository onOpened={onOpened} />}
      {mode === 'init' && <InitRepository onOpened={onOpened} />}
    </div>
  );
}
