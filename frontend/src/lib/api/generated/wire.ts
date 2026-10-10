// Generated from doc/openapi.json; do not edit.
// SHA-256: 75c07bcf5a2aac8d8f525d8a4bfd00e38ba3bfbf6418ebc54f98d33f42e556e1
export interface components {
  schemas: {
    CatalogSummary: {
      revision: components['schemas']['Decimal'];
      scope: 'current';
      disk_count: components['schemas']['Decimal'];
      file_count: components['schemas']['Decimal'];
      content_count: components['schemas']['Decimal'];
    };
    DirectorySummary: {
      path: string;
      file_count: components['schemas']['Decimal'];
      content_count: components['schemas']['Decimal'];
      known_bytes: components['schemas']['Decimal'];
      unknown_size_file_count: components['schemas']['Decimal'];
      size_complete: boolean;
      unique_content_known_bytes: components['schemas']['Decimal'];
      unknown_size_content_count: components['schemas']['Decimal'];
      unique_content_size_complete: boolean;
      max_known_mtime: string | null;
    };
    DirectoryDetail: {
      snapshot: components['schemas']['Snapshot'];
      revision: components['schemas']['Decimal'];
      is_current: boolean;
      replica_scope: 'current';
      directory: components['schemas']['DirectorySummary'];
      redundancy_histogram: Array<{
        other_disk_count: components['schemas']['Decimal'];
        file_count: components['schemas']['Decimal'];
      }>;
    };
    DirectoryPage: {
      snapshot: components['schemas']['Snapshot'];
      revision: components['schemas']['Decimal'];
      is_current: boolean;
      replica_scope: 'current';
      filters: {
        path: string;
        directories_only: boolean;
        recursive: boolean;
        replica_metric: 'disks' | 'locations';
        other_replicas: string | null;
        min_other_replicas: string | null;
        max_other_replicas: string | null;
      };
      items: Array<components['schemas']['DirectoryEntry']>;
      next_cursor: string | null;
    };
    DirectoryEntry: {
      path: string;
      kind: 'directory' | 'file';
      directory: {
        path: string;
        file_count: components['schemas']['Decimal'];
        content_count: components['schemas']['Decimal'];
        known_bytes: components['schemas']['Decimal'];
        unknown_size_file_count: components['schemas']['Decimal'];
        size_complete: boolean;
        unique_content_known_bytes: components['schemas']['Decimal'];
        unknown_size_content_count: components['schemas']['Decimal'];
        unique_content_size_complete: boolean;
        max_known_mtime: string | null;
      } | null;
      file: {
        observation: components['schemas']['Observation'];
        hash: components['schemas']['Hash'];
        size: string | null;
        other_location_count: components['schemas']['Decimal'];
        other_disk_count: components['schemas']['Decimal'];
      } | null;
    };
    DirectoryComparisonFilters: { path: string; allow: Array<string>; block: Array<string> };
    DirectorySelection: {
      retained_file_count: components['schemas']['Count'];
      excluded_file_count: components['schemas']['Count'];
      content_count: components['schemas']['Count'];
      known_bytes: components['schemas']['Decimal'];
      unknown_size_file_count: components['schemas']['Count'];
      size_complete: boolean;
      empty_comparison: boolean;
    };
    DirectoryReplica: {
      disk: components['schemas']['Disk'];
      snapshot: components['schemas']['Snapshot'];
      path: string;
      same_disk: boolean;
      whole_tree_equal: boolean;
      retained_file_count: components['schemas']['Count'];
      excluded_file_count: components['schemas']['Count'];
    };
    DirectoryReplicaPage: {
      snapshot: components['schemas']['Snapshot'];
      revision: components['schemas']['Decimal'];
      is_current: boolean;
      replica_scope: 'current';
      filters: components['schemas']['DirectoryComparisonFilters'];
      selection: components['schemas']['DirectorySelection'];
      items: Array<components['schemas']['DirectoryReplica']>;
      next_cursor: string | null;
    };
    DirectoryCoverage: {
      disk: components['schemas']['Disk'];
      snapshot: components['schemas']['Snapshot'];
      covered_file_count: components['schemas']['Count'];
      missing_file_count: components['schemas']['Count'];
      covered_content_count: components['schemas']['Count'];
      missing_content_count: components['schemas']['Count'];
      covered_known_bytes: components['schemas']['Decimal'];
      missing_known_bytes: components['schemas']['Decimal'];
      covered_unknown_size_file_count: components['schemas']['Count'];
      missing_unknown_size_file_count: components['schemas']['Count'];
      complete: boolean;
    };
    DirectoryCoveragePage: {
      snapshot: components['schemas']['Snapshot'];
      revision: components['schemas']['Decimal'];
      is_current: boolean;
      replica_scope: 'current';
      filters: components['schemas']['DirectoryComparisonFilters'];
      selection: components['schemas']['DirectorySelection'];
      items: Array<components['schemas']['DirectoryCoverage']>;
      next_cursor: string | null;
    };
    CreateDisk: { label: string; capacity: string; notes?: string | null; serial?: string | null };
    UpdateDisk: {
      label?: string;
      capacity?: string;
      notes?: string | null;
      serial?: string | null;
    };
    DiskSummary: {
      id: components['schemas']['ID'];
      label: string;
      notes: string | null;
      serial: string | null;
      capacity: components['schemas']['Decimal'];
      latest_snapshot: components['schemas']['NullableSnapshot'];
    };
    DiskDetail: {
      id: components['schemas']['ID'];
      label: string;
      notes: string | null;
      serial: string | null;
      capacity: components['schemas']['Decimal'];
      latest_snapshot: components['schemas']['NullableSnapshot'];
      cataloged: {
        file_count: components['schemas']['Count'];
        content_count: components['schemas']['Count'];
        known_bytes: components['schemas']['Decimal'];
        unknown_size_file_count: components['schemas']['Count'];
        size_complete: boolean;
      } | null;
    };
    DiskPage: {
      revision: components['schemas']['Decimal'];
      items: Array<components['schemas']['DiskSummary']>;
      next_cursor: string | null;
    };
    NullableSnapshot: {
      id: components['schemas']['ID'];
      disk_id: components['schemas']['ID'];
      state: 'complete';
      captured_at: components['schemas']['Timestamp'];
      imported_at: components['schemas']['Timestamp'];
      capture_provenance: 'explicit' | 'source_mtime';
      input_path: string;
      input_format: string;
      file_count: components['schemas']['Count'];
      content_count: components['schemas']['Count'];
    } | null;
    Decimal: string;
    ContentPage: {
      scope: 'current' | 'history';
      revision: components['schemas']['Decimal'];
      filters: {
        disk_id: string | null;
        directory: string;
        replica_metric: 'disks' | 'locations';
        other_replicas: string | null;
        min_other_replicas: string | null;
        max_other_replicas: string | null;
      };
      items: Array<components['schemas']['Content']>;
      next_cursor: string | null;
    };
    SearchPage: {
      scope: 'current' | 'history';
      revision: components['schemas']['Decimal'];
      filters: {
        q: string;
        field: 'name' | 'path';
        match: 'exact' | 'substring';
        snapshot_id: string | null;
        disk_id: string | null;
        directory: string;
        replica_metric: 'disks' | 'locations';
        other_replicas: string | null;
        min_other_replicas: string | null;
        max_other_replicas: string | null;
      };
      items: Array<components['schemas']['SearchItem']>;
      next_cursor: string | null;
    };
    SearchItem: {
      observation: components['schemas']['Observation'];
      content: components['schemas']['Content'];
      disk: components['schemas']['Disk'];
      snapshot: components['schemas']['Snapshot'];
      basename: string;
      is_current: boolean;
      relevance: 'exact' | 'prefix' | 'substring';
    };
    SnapshotPage: {
      disk_id: components['schemas']['ID'];
      revision: components['schemas']['Count'];
      items: Array<components['schemas']['Snapshot']>;
      next_cursor: string | null;
    };
    ID: string;
    Count: string;
    NullableBytes: string | null;
    Timestamp: string;
    NullableTimestamp: string | null;
    Algorithm:
      | 'md4'
      | 'md5'
      | 'sha1'
      | 'sha256'
      | 'sha384'
      | 'sha3_224'
      | 'sha3_256'
      | 'sha3_384'
      | 'sha3_512'
      | 'sha512';
    Hash: { algorithm: components['schemas']['Algorithm']; hex: string };
    Content: {
      id: components['schemas']['ID'];
      hash: components['schemas']['Hash'];
      size: components['schemas']['NullableBytes'];
      scope: 'current' | 'history';
      location_count: components['schemas']['Count'];
      disk_count: components['schemas']['Count'];
      observation_count: components['schemas']['Count'];
      current_location_count: components['schemas']['Count'];
      current_disk_count: components['schemas']['Count'];
    };
    Disk: {
      id: components['schemas']['ID'];
      label: string;
      serial: string;
      capacity: components['schemas']['Count'];
    };
    Snapshot: {
      id: components['schemas']['ID'];
      disk_id: components['schemas']['ID'];
      state: 'complete';
      captured_at: components['schemas']['Timestamp'];
      imported_at: components['schemas']['Timestamp'];
      capture_provenance: 'explicit' | 'source_mtime';
      input_path: string;
      input_format: string;
      file_count: components['schemas']['Count'];
      content_count: components['schemas']['Count'];
    };
    Observation: {
      id: components['schemas']['ID'];
      content_id: components['schemas']['ID'];
      snapshot_id: components['schemas']['ID'];
      path: string;
      mtime: components['schemas']['NullableTimestamp'];
    };
    ObservationSummary: {
      observation: components['schemas']['Observation'];
      snapshot: components['schemas']['Snapshot'];
      disk: components['schemas']['Disk'];
      content: components['schemas']['Content'];
      other_location_count: components['schemas']['Count'];
      other_disk_count: components['schemas']['Count'];
    };
    ContentObservation: {
      observation: components['schemas']['Observation'];
      snapshot: components['schemas']['Snapshot'];
      disk: components['schemas']['Disk'];
      size: components['schemas']['NullableBytes'];
      is_current: boolean;
    };
    ObservationPage: {
      scope: 'current' | 'history';
      revision: components['schemas']['Count'];
      items: Array<components['schemas']['ContentObservation']>;
      next_cursor: string | null;
    };
    Error: {
      error: {
        code:
          | 'invalid_request'
          | 'not_found'
          | 'stale_cursor'
          | 'conflict'
          | 'catalog_unavailable'
          | 'internal_error'
          | 'method_not_allowed'
          | 'request_too_large'
          | 'unsupported_media_type';
        message: string;
      };
    };
  };
}
export interface operations {
  'GET /api/v1/catalog': {
    query: Record<string, never>;
    body: never;
    response: components['schemas']['CatalogSummary'];
  };
  'GET /api/v1/snapshots/{id}/directories': {
    query: { parent?: string; limit?: number; cursor?: string };
    body: never;
    response: components['schemas']['DirectoryPage'];
  };
  'GET /api/v1/snapshots/{id}/directory': {
    query: { path?: string };
    body: never;
    response: components['schemas']['DirectoryDetail'];
  };
  'GET /api/v1/snapshots/{id}/directory/entries': {
    query: {
      path?: string;
      recursive?: boolean;
      replica_metric?: 'disks' | 'locations';
      other_replicas?: components['schemas']['Decimal'];
      min_other_replicas?: components['schemas']['Decimal'];
      max_other_replicas?: components['schemas']['Decimal'];
      limit?: number;
      cursor?: string;
    };
    body: never;
    response: components['schemas']['DirectoryPage'];
  };
  'GET /api/v1/snapshots/{id}/directory/replicas': {
    query: {
      path?: string;
      allow?: Array<string>;
      block?: Array<string>;
      limit?: number;
      cursor?: string;
    };
    body: never;
    response: components['schemas']['DirectoryReplicaPage'];
  };
  'GET /api/v1/snapshots/{id}/directory/coverage': {
    query: {
      path?: string;
      allow?: Array<string>;
      block?: Array<string>;
      limit?: number;
      cursor?: string;
    };
    body: never;
    response: components['schemas']['DirectoryCoveragePage'];
  };
  'GET /api/v1/search': {
    query: {
      q: string;
      field?: 'name' | 'path';
      match?: 'exact' | 'substring';
      scope?: 'current' | 'history';
      disk_id?: components['schemas']['ID'];
      snapshot_id?: components['schemas']['ID'];
      directory?: string;
      replica_metric?: 'disks' | 'locations';
      other_replicas?: components['schemas']['Decimal'];
      min_other_replicas?: components['schemas']['Decimal'];
      max_other_replicas?: components['schemas']['Decimal'];
      limit?: number;
      cursor?: string;
    };
    body: never;
    response: components['schemas']['SearchPage'];
  };
  'GET /healthz': { query: Record<string, never>; body: never; response: { status: 'ready' } };
  'GET /api/v1/contents': {
    query: {
      scope?: 'current' | 'history';
      disk_id?: components['schemas']['ID'];
      directory?: string;
      replica_metric?: 'disks' | 'locations';
      other_replicas?: components['schemas']['Decimal'];
      min_other_replicas?: components['schemas']['Decimal'];
      max_other_replicas?: components['schemas']['Decimal'];
      limit?: number;
      cursor?: string;
    };
    body: never;
    response: components['schemas']['ContentPage'];
  };
  'GET /api/v1/contents/lookup': {
    query: {
      hash_type: components['schemas']['Algorithm'];
      hash: string;
      scope?: 'current' | 'history';
    };
    body: never;
    response: components['schemas']['Content'];
  };
  'GET /api/v1/contents/{id}': {
    query: { scope?: 'current' | 'history' };
    body: never;
    response: components['schemas']['Content'];
  };
  'GET /api/v1/contents/{id}/observations': {
    query: { scope?: 'current' | 'history'; limit?: number; cursor?: string };
    body: never;
    response: components['schemas']['ObservationPage'];
  };
  'GET /api/v1/observations/{id}': {
    query: Record<string, never>;
    body: never;
    response: components['schemas']['ObservationSummary'];
  };
  'GET /api/v1/disks': {
    query: { limit?: number; cursor?: string };
    body: never;
    response: components['schemas']['DiskPage'];
  };
  'POST /api/v1/disks': {
    query: Record<string, never>;
    body: components['schemas']['CreateDisk'];
    response: components['schemas']['DiskDetail'];
  };
  'GET /api/v1/disks/{id}': {
    query: Record<string, never>;
    body: never;
    response: components['schemas']['DiskDetail'];
  };
  'PATCH /api/v1/disks/{id}': {
    query: Record<string, never>;
    body: components['schemas']['UpdateDisk'];
    response: components['schemas']['DiskDetail'];
  };
  'GET /api/v1/disks/{id}/snapshots': {
    query: { limit?: number; cursor?: string };
    body: never;
    response: components['schemas']['SnapshotPage'];
  };
  'GET /api/v1/snapshots/{id}': {
    query: Record<string, never>;
    body: never;
    response: components['schemas']['Snapshot'];
  };
}
