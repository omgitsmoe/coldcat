import type { components } from '../../src/lib/api/generated/wire';
import { searchFixture } from './api';

export function contentFixture(): components['schemas']['Content'] {
  return {
    ...searchFixture().items[0]!.content,
    scope: 'history',
    location_count: '2',
    disk_count: '1',
    observation_count: '5',
  };
}

export function locationsFixture(): components['schemas']['ObservationPage'] {
  const { observation, snapshot, disk, content } = searchFixture().items[0]!;
  return {
    scope: 'current',
    revision: '9007199254740993',
    items: [
      { observation, snapshot, disk, size: content.size, is_current: true },
      {
        observation: { ...observation, id: '2', path: 'copy.txt' },
        snapshot,
        disk,
        size: content.size,
        is_current: true,
      },
    ],
    next_cursor: null,
  };
}

export function observationFixture(): components['schemas']['ObservationSummary'] {
  const { observation, snapshot, disk, content } = searchFixture().items[0]!;
  return { observation, snapshot, disk, content, other_location_count: '1', other_disk_count: '0' };
}
