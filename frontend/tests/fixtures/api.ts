import type { components } from '../../src/lib/api/generated/wire';

export function diskFixture(): components['schemas']['DiskDetail'] {
  return {
    id: '9007199254740993',
    label: 'archive',
    notes: 'offline',
    serial: 'ABC',
    capacity: '9223372036854775807',
    latest_snapshot: null,
    cataloged: null,
  };
}

export function searchFixture(): components['schemas']['SearchPage'] {
  return {
    scope: 'current',
    revision: '9007199254740993',
    filters: {
      q: 'report',
      field: 'name',
      match: 'substring',
      snapshot_id: null,
      disk_id: null,
      directory: '',
      replica_metric: 'disks',
      other_replicas: null,
      min_other_replicas: null,
      max_other_replicas: null,
    },
    items: [
      {
        observation: {
          id: '9007199254740993',
          content_id: '1',
          snapshot_id: '1',
          path: 'É/<report>&?.txt',
          mtime: null,
        },
        content: {
          id: '1',
          hash: { algorithm: 'sha256', hex: 'ab'.repeat(32) },
          size: '0',
          scope: 'current',
          location_count: '2',
          disk_count: '1',
          observation_count: '2',
          current_location_count: '2',
          current_disk_count: '1',
        },
        disk: { id: '1', label: '<archive>', serial: '', capacity: '9007199254740993' },
        snapshot: {
          id: '1',
          disk_id: '1',
          state: 'complete',
          captured_at: '1970-01-01T00:00:10.000000123Z',
          imported_at: '2026-10-10T12:00:00Z',
          capture_provenance: 'explicit',
          input_path: '/source.cshd',
          input_format: 'cshd',
          file_count: '2',
          content_count: '1',
        },
        basename: '<report>&?.txt',
        is_current: true,
        relevance: 'substring',
      },
    ],
    next_cursor: null,
  };
}
