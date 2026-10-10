import { describe, expect, it, vi } from 'vitest';
import contract from '../../../doc/openapi.json';
import { createClient } from '../../src/lib/api/client';
import { ApiError } from '../../src/lib/api/errors';
import { buildDiskPatch } from '../../src/lib/api/disks';
import { diskFixture, searchFixture } from '../fixtures/api';

function resolve<T>(value: T): T {
  if (value && typeof value === 'object' && '$ref' in value) {
    return String(value.$ref)
      .split('/')
      .slice(1)
      .reduce<unknown>((node, key) => (node as Record<string, unknown>)[key], contract) as T;
  }
  return value;
}

const cases = [
  ['get', '/healthz', (c: ReturnType<typeof createClient>, s: AbortSignal) => c.health(s)],
  ['get', '/api/v1/catalog', (c: ReturnType<typeof createClient>, s: AbortSignal) => c.catalog(s)],
  [
    'get',
    '/api/v1/search',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.search({ q: 'report' }, s),
  ],
  [
    'get',
    '/api/v1/contents',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.contents({}, s),
  ],
  [
    'get',
    '/api/v1/contents/lookup',
    (c: ReturnType<typeof createClient>, s: AbortSignal) =>
      c.lookupContent({ hash_type: 'sha256', hash: 'ab'.repeat(32) }, s),
  ],
  [
    'get',
    '/api/v1/contents/{id}',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.content('1', {}, s),
  ],
  [
    'get',
    '/api/v1/contents/{id}/observations',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.contentObservations('1', {}, s),
  ],
  [
    'get',
    '/api/v1/observations/{id}',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.observation('1', s),
  ],
  ['get', '/api/v1/disks', (c: ReturnType<typeof createClient>, s: AbortSignal) => c.disks({}, s)],
  [
    'post',
    '/api/v1/disks',
    (c: ReturnType<typeof createClient>, s: AbortSignal) =>
      c.createDisk({ label: 'archive', capacity: '0' }, s),
  ],
  [
    'get',
    '/api/v1/disks/{id}',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.disk('1', s),
  ],
  [
    'patch',
    '/api/v1/disks/{id}',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.updateDisk('1', { notes: null }, s),
  ],
  [
    'get',
    '/api/v1/disks/{id}/snapshots',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.diskSnapshots('1', {}, s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.snapshot('1', s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}/directories',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.directories('1', {}, s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}/directory',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.directory('1', {}, s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}/directory/entries',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.directoryEntries('1', {}, s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}/directory/replicas',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.directoryReplicas('1', {}, s),
  ],
  [
    'get',
    '/api/v1/snapshots/{id}/directory/coverage',
    (c: ReturnType<typeof createClient>, s: AbortSignal) => c.directoryCoverage('1', {}, s),
  ],
] as const;

describe('explicit endpoint wrappers', () => {
  it('covers every documented operation exactly once', () => {
    const operations = Object.entries(contract.paths).flatMap(([path, methods]) =>
      Object.keys(methods).map((method) => `${method} ${path}`),
    );
    expect(cases.map(([method, path]) => `${method} ${path}`).sort()).toEqual(operations.sort());
  });

  for (const [method, path, call] of cases) {
    it(`${method} ${path}: accepts every success fixture, passes signal and method`, async () => {
      const operation = (
        contract.paths as Record<string, Record<string, { responses: Record<string, unknown> }>>
      )[path]![method]!;
      for (const [status, raw] of Object.entries(operation.responses)) {
        if (!status.startsWith('2')) continue;
        const response = resolve(raw) as {
          content: {
            'application/json': {
              example?: unknown;
              examples?: Record<string, { value: unknown }>;
            };
          };
        };
        const media = response.content['application/json'];
        const examples = media.examples
          ? Object.values(media.examples).map((e) => e.value)
          : [media.example];
        expect(examples.length).toBeGreaterThan(0);
        for (const example of examples) {
          const fetcher = vi
            .fn()
            .mockResolvedValue(Response.json(example, { status: Number(status) }));
          const signal = new AbortController().signal;
          expect(await call(createClient(fetcher), signal)).toEqual(example);
          const [url, init] = fetcher.mock.calls[0]!;
          expect(new URL(url, 'http://localhost').pathname).toBe(path.replace('{id}', '1'));
          expect(init.signal).toBe(signal);
          expect(init.method).toBe(method.toUpperCase());
          if (method !== 'get') {
            expect(init.headers['Content-Type']).toBe('application/json');
            expect(JSON.parse(init.body)).toEqual(
              method === 'post' ? { label: 'archive', capacity: '0' } : { notes: null },
            );
          }
        }
      }
    });
  }

  it('encodes literal queries, root, huge IDs, false, opaque cursors and repeated globs', async () => {
    const fetcher = vi.fn().mockResolvedValue(Response.json(searchFixture()));
    const client = createClient(fetcher);
    await client.search({
      q: ' é +&#?\\ ',
      directory: '',
      disk_id: '9007199254740993',
      cursor: 'opaque+/=?&',
      limit: 50,
      match: 'exact',
    });
    const query = new URL(fetcher.mock.calls[0]![0], 'http://localhost').searchParams;
    expect(Object.fromEntries(query)).toEqual({
      q: ' é +&#?\\ ',
      directory: '',
      disk_id: '9007199254740993',
      cursor: 'opaque+/=?&',
      limit: '50',
      match: 'exact',
    });

    const raw =
      contract.paths['/api/v1/snapshots/{id}/directory/replicas'].get.responses['200'].content[
        'application/json'
      ].example;
    fetcher.mockResolvedValue(Response.json(raw));
    await client.directoryReplicas('9007199254740993', {
      path: 'é/+&#?\\',
      allow: ['**/a\\?.txt', ' spaced '],
      block: ['**/*.log', '**/*.log'],
    });
    const url = new URL(fetcher.mock.calls[1]![0], 'http://localhost');
    expect(url.pathname).toContain('9007199254740993');
    expect(url.searchParams.get('path')).toBe('é/+&#?\\');
    expect(url.searchParams.getAll('allow')).toEqual(['**/a\\?.txt', ' spaced ']);
    expect(url.searchParams.getAll('block')).toEqual(['**/*.log', '**/*.log']);

    fetcher.mockResolvedValue(
      Response.json(
        contract.paths['/api/v1/snapshots/{id}/directory/entries'].get.responses['200'].content[
          'application/json'
        ].example,
      ),
    );
    await client.directoryEntries('1', { recursive: false, other_replicas: '0' });
    expect(fetcher.mock.calls[2]![0]).toContain('recursive=false');
    expect(fetcher.mock.calls[2]![0]).toContain('other_replicas=0');
  });

  it('rejects undeclared query keys rather than forwarding them', async () => {
    const fetcher = vi.fn();
    await expect(
      createClient(fetcher).disks({ limit: 50, rogue: 'x' } as { limit: number }),
    ).rejects.toMatchObject({ kind: 'request' });
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('retains an explicit root parent and omits absent scalars', async () => {
    const example =
      contract.paths['/api/v1/snapshots/{id}/directories'].get.responses['200'].content[
        'application/json'
      ].example;
    const fetcher = vi.fn().mockResolvedValue(Response.json(example));
    await createClient(fetcher).directories('1', { parent: '', cursor: undefined });
    expect(fetcher.mock.calls[0]![0]).toBe('/api/v1/snapshots/1/directories?parent=');
  });

  it('fails invalid IDs, malformed literal input and invalid writes before dispatch', async () => {
    const fetcher = vi.fn();
    const client = createClient(fetcher);
    for (const id of ['0', '01', '1\n', '1/../2', '9223372036854775808']) {
      await expect(client.disk(id)).rejects.toMatchObject({ kind: 'request' });
    }
    await expect(client.search({ q: '\ud800' })).rejects.toMatchObject({ kind: 'request' });
    await expect(client.search({ q: 'a\0b' })).rejects.toMatchObject({ kind: 'request' });
    await expect(client.createDisk({ label: '', capacity: '0' })).rejects.toMatchObject({
      kind: 'request',
    });
    await expect(
      client.createDisk({ label: 'archive', capacity: '9223372036854775808' }),
    ).rejects.toMatchObject({ kind: 'request' });
    await expect(
      client.createDisk({ label: 'archive', capacity: '0', notes: 'a'.repeat(65536) }),
    ).rejects.toMatchObject({ kind: 'request' });
    await expect(
      client.updateDisk('1', { label: null } as unknown as { label: string }),
    ).rejects.toMatchObject({ kind: 'request' });
    await expect(client.directoryCoverage('1', { allow: ['é'.repeat(513)] })).rejects.toMatchObject(
      { kind: 'request' },
    );
    expect(fetcher).not.toHaveBeenCalled();
  });
});

describe('response boundary and errors', () => {
  it('preserves huge decimal text and null/zero without modifying serializable wire objects', async () => {
    const fixture = searchFixture();
    const client = createClient(vi.fn().mockResolvedValue(Response.json(fixture)));
    expect(await client.search({ q: 'report' })).toEqual(fixture);
    expect(JSON.stringify(fixture)).toContain('9007199254740993');
    const unknown = searchFixture();
    unknown.items[0]!.content.size = null;
    expect(
      await createClient(vi.fn().mockResolvedValue(Response.json(unknown))).search({ q: 'report' }),
    ).toEqual(unknown);
  });

  it('rejects a directory entry with inconsistent kind/null branches', async () => {
    const example = structuredClone(
      contract.paths['/api/v1/snapshots/{id}/directory/entries'].get.responses['200'].content[
        'application/json'
      ].example,
    );
    example.items[0]!.kind = 'directory';
    await expect(
      createClient(vi.fn().mockResolvedValue(Response.json(example))).directoryEntries('1'),
    ).rejects.toMatchObject({ kind: 'contract' });
  });

  it('allows undeclared response properties only where the schema permits them', async () => {
    const fixture = { ...searchFixture(), toString: 'literal' };
    await expect(
      createClient(vi.fn().mockResolvedValue(Response.json(fixture))).search({ q: 'report' }),
    ).resolves.toEqual(fixture);
    await expect(
      createClient(
        vi.fn().mockResolvedValue(
          Response.json({
            revision: '0',
            scope: 'current',
            disk_count: '0',
            file_count: '0',
            content_count: '0',
            extra: true,
          }),
        ),
      ).catalog(),
    ).rejects.toMatchObject({ kind: 'contract' });
  });

  it.each([
    (v: ReturnType<typeof searchFixture>) => {
      delete (v as Partial<typeof v>).revision;
    },
    (v: ReturnType<typeof searchFixture>) => {
      Object.assign(v, { revision: 9007199254740992 });
    },
    (v: ReturnType<typeof searchFixture>) => {
      v.revision = '-1';
    },
    (v: ReturnType<typeof searchFixture>) => {
      v.revision = '01';
    },
    (v: ReturnType<typeof searchFixture>) => {
      v.revision = '9223372036854775808';
    },
    (v: ReturnType<typeof searchFixture>) => {
      Object.assign(v, { items: null });
    },
    (v: ReturnType<typeof searchFixture>) => {
      delete (v as Partial<typeof v>).next_cursor;
    },
    (v: ReturnType<typeof searchFixture>) => {
      v.next_cursor = '';
    },
    (v: ReturnType<typeof searchFixture>) => {
      v.items[0]!.snapshot.captured_at = '2026-02-30T12:00:00Z';
    },
    (v: ReturnType<typeof searchFixture>) => {
      delete (
        v.items[0]!.content as Partial<ReturnType<typeof searchFixture>['items'][number]['content']>
      ).size;
    },
  ])('rejects broken required/shared invariants', async (mutate) => {
    const fixture = searchFixture();
    mutate(fixture);
    await expect(
      createClient(vi.fn().mockResolvedValue(Response.json(fixture))).search({ q: 'report' }),
    ).rejects.toMatchObject({ kind: 'contract', status: 200 });
  });

  it.each([400, 404, 409, 500, 503])(
    'retains structured HTTP status/code/message (%i)',
    async (status) => {
      const error = {
        code: status === 409 ? 'stale_cursor' : 'internal_error',
        message: '<b>literal</b>',
      };
      await expect(
        createClient(vi.fn().mockResolvedValue(Response.json({ error }, { status }))).catalog(),
      ).rejects.toMatchObject({ kind: 'http', status, code: error.code, message: error.message });
    },
  );

  it.each([200, 502, 504])(
    'distinguishes HTML success from proxy failure, retains status %i',
    async (status) => {
      await expect(
        createClient(
          vi.fn().mockResolvedValue(
            new Response('<html>proxy</html>', {
              status,
              headers: { 'Content-Type': 'text/html' },
            }),
          ),
        ).catalog(),
      ).rejects.toMatchObject({ kind: status === 200 ? 'contract' : 'transport', status });
    },
  );

  it('distinguishes JSON parse failure, malformed HTTP error and unexpected success status', async () => {
    for (const response of [
      new Response('{', { headers: { 'Content-Type': 'application/json' } }),
      Response.json({ error: { code: 'stale_cursor' } }, { status: 409 }),
      Response.json({ status: 'ready' }, { status: 201 }),
    ]) {
      const failure = createClient(vi.fn().mockResolvedValue(response)).health();
      await expect(failure).rejects.toBeInstanceOf(ApiError);
      await expect(failure).rejects.toMatchObject({ kind: 'contract' });
    }
  });

  it('does not retry uncertain writes or body-stream transport failures', async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError('offline'));
    await expect(
      createClient(fetcher).createDisk({ label: 'archive', capacity: '0' }),
    ).rejects.toMatchObject({ kind: 'transport', uncertainWrite: true });
    expect(fetcher).toHaveBeenCalledTimes(1);
    const response = Response.json({ status: 'ready' });
    vi.spyOn(response, 'text').mockRejectedValue(new TypeError('stream lost'));
    await expect(createClient(vi.fn().mockResolvedValue(response)).health()).rejects.toMatchObject({
      kind: 'transport',
    });
  });

  it('distinguishes cancellation before fetch, during fetch and while reading the body', async () => {
    const controller = new AbortController();
    controller.abort();
    const fetcher = vi.fn();
    await expect(createClient(fetcher).catalog(controller.signal)).rejects.toMatchObject({
      kind: 'cancelled',
    });
    expect(fetcher).not.toHaveBeenCalled();
    await expect(
      createClient(vi.fn().mockRejectedValue(new DOMException('cancel', 'AbortError'))).catalog(),
    ).rejects.toMatchObject({ kind: 'cancelled' });
    const late = new AbortController();
    const response = Response.json({ status: 'ready' });
    vi.spyOn(response, 'text').mockImplementation(async () => {
      late.abort('obsolete');
      return '{"status":"ready"}';
    });
    await expect(
      createClient(vi.fn().mockResolvedValue(response)).health(late.signal),
    ).rejects.toMatchObject({ kind: 'cancelled' });
  });
});

describe('changed-only disk PATCH', () => {
  it('omits unchanged fields, retains null/empty clearing and literal changed text', () => {
    const before = diskFixture();
    expect(
      buildDiskPatch(before, {
        label: before.label,
        capacity: before.capacity,
        notes: null,
        serial: '',
      }),
    ).toEqual({ notes: null, serial: '' });
    expect(buildDiskPatch(before, { label: ' new ', capacity: '9007199254740993' })).toEqual({
      label: ' new ',
      capacity: '9007199254740993',
    });
    expect(buildDiskPatch(before, before)).toEqual({});
    expect(
      buildDiskPatch(
        { ...before, notes: null, serial: null, capacity: '1' },
        {
          notes: '',
          serial: '',
          capacity: '0001',
        },
      ),
    ).toEqual({});
  });

  it('does not submit an empty PATCH', async () => {
    const fetcher = vi.fn();
    await expect(createClient(fetcher).updateDisk('1', {})).rejects.toMatchObject({
      kind: 'request',
    });
    expect(fetcher).not.toHaveBeenCalled();
  });
});
