import type { components } from './generated/wire';
import { capacityInput } from '../format/decimal';

export type DiskMetadata = Pick<
  components['schemas']['DiskDetail'],
  'label' | 'capacity' | 'notes' | 'serial'
>;

function clearedText(value: string | null): string | null {
  return value === '' ? null : value;
}

export function buildDiskPatch(
  before: DiskMetadata,
  draft: Partial<DiskMetadata>,
): components['schemas']['UpdateDisk'] {
  const patch: components['schemas']['UpdateDisk'] = {};
  if (draft.label !== undefined && draft.label !== before.label) patch.label = draft.label;
  if (draft.capacity !== undefined) {
    const capacity = capacityInput(draft.capacity);
    if (capacity !== before.capacity) patch.capacity = capacity;
  }
  if (draft.notes !== undefined && clearedText(draft.notes) !== clearedText(before.notes))
    patch.notes = draft.notes;
  if (draft.serial !== undefined && clearedText(draft.serial) !== clearedText(before.serial))
    patch.serial = draft.serial;
  return patch;
}
