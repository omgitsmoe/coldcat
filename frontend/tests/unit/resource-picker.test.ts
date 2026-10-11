import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ResourcePicker from '../../src/lib/components/ResourcePicker.svelte';
import { createClient } from '../../src/lib/api/client';
import { Connection } from '../../src/lib/state/connection';
import { ApiError } from '../../src/lib/api/errors';
import { diskFixture, searchFixture } from '../fixtures/api';

const context = vi.hoisted(() => ({ connection: undefined as unknown as Connection }));
vi.mock('../../src/lib/state/context', () => ({ useConnection: () => context.connection }));
let connection: Connection;
beforeEach(() => {
  connection = new Connection(createClient());
  context.connection = connection;
});
afterEach(() => {
  cleanup();
  connection.dispose();
});
const diskPage = (id = '1', next_cursor: string | null = null) => ({
  revision: '3',
  items: [{ ...diskFixture(), id, label: `雪 <disk ${id}>` }],
  next_cursor,
});
const inventoryPage = (id = '3', next_cursor: string | null = null) => ({
  disk_id: '1',
  revision: '3',
  items: [{ ...searchFixture().items[0]!.snapshot, id }],
  next_cursor,
});

describe('independent lazy resource picker', () => {
  it('does not preload or resolve bookmarked IDs, and selection edits once without applying', async () => {
    connection.client.disks = vi.fn().mockResolvedValue(diskPage());
    const edited = vi.fn();
    render(ResourcePicker, { value: '9007199254740993', edited });
    expect(connection.client.disks).not.toHaveBeenCalled();
    expect(screen.getByText(/unknown \/ not verified/)).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Choose disk' }));
    await screen.findByRole('button', { name: 'Use disk 1' });
    expect(connection.client.disks).toHaveBeenCalledWith(
      { limit: 25, cursor: undefined },
      expect.any(AbortSignal),
    );
    expect(edited).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Use disk 1' }));
    expect(edited).toHaveBeenCalledTimes(1);
    expect((screen.getByRole('textbox', { name: 'Disk ID' }) as HTMLInputElement).value).toBe('1');
    await fireEvent.click(screen.getByRole('button', { name: 'Use disk 1' }));
    expect(edited).toHaveBeenCalledTimes(1);
    await fireEvent.click(screen.getByRole('button', { name: 'Close disk' }));
    expect(edited).toHaveBeenCalledTimes(1);
    expect(connection.client.disks).toHaveBeenCalledTimes(1);
  });

  it('pages explicitly, stops accumulation at ten pages and navigates retained options', async () => {
    connection.client.disks = vi.fn().mockImplementation(({ cursor }) => {
      const n = Number(cursor ?? 1);
      return Promise.resolve(diskPage(String(n), n === 12 ? null : String(n + 1)));
    });
    render(ResourcePicker, { value: '', edited: vi.fn() });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose disk' }));
    await screen.findByRole('button', { name: 'Use disk 1' });
    expect(connection.client.disks).toHaveBeenCalledTimes(1);
    for (let n = 2; n <= 10; n++) {
      await fireEvent.click(screen.getByRole('button', { name: 'Load more options' }));
      await screen.findByRole('button', { name: `Use disk ${n}` });
    }
    expect(screen.getAllByRole('button', { name: /^Use disk/ })).toHaveLength(10);
    await fireEvent.click(screen.getByRole('button', { name: 'Next options' }));
    await screen.findByRole('button', { name: 'Use disk 11' });
    expect(screen.getAllByRole('button', { name: /^Use disk/ })).toHaveLength(1);
    await fireEvent.click(screen.getByRole('button', { name: 'Previous options' }));
    await screen.findByRole('button', { name: 'Use disk 10' });
    expect(connection.client.disks).toHaveBeenCalledTimes(11);
  });

  it('preserves direct inventory ID without a disk; disk change aborts obsolete history', async () => {
    let release!: (value: ReturnType<typeof inventoryPage>) => void;
    connection.client.diskSnapshots = vi.fn().mockImplementation((id) =>
      id === '1'
        ? new Promise((resolve) => {
            release = resolve;
          })
        : Promise.resolve(inventoryPage('8')),
    );
    const edited = vi.fn();
    const mounted = render(ResourcePicker, { kind: 'inventory', diskID: '', value: '99', edited });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose inventory' }));
    expect(connection.client.diskSnapshots).not.toHaveBeenCalled();
    expect(screen.getByText(/Enter a valid Disk ID first/)).toBeTruthy();
    await mounted.rerender({ diskID: '1' });
    await waitFor(() => expect(connection.client.diskSnapshots).toHaveBeenCalledTimes(1));
    const signal = vi.mocked(connection.client.diskSnapshots).mock.calls[0]![2]!;
    await mounted.rerender({ diskID: '2' });
    await screen.findByRole('button', { name: 'Use inventory 8' });
    expect(signal.aborted).toBe(true);
    release(inventoryPage('3'));
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Use inventory 3' })).toBeNull(),
    );
    expect((screen.getByRole('textbox', { name: 'Snapshot ID' }) as HTMLInputElement).value).toBe(
      '99',
    );
    expect(edited).not.toHaveBeenCalled();
  });

  it('labels older captures from the first page without changing scope', async () => {
    connection.client.diskSnapshots = vi
      .fn()
      .mockResolvedValueOnce(inventoryPage('3', 'older'))
      .mockResolvedValueOnce(inventoryPage('2'));
    const edited = vi.fn();
    render(ResourcePicker, { kind: 'inventory', diskID: '1', value: '2', edited });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose inventory' }));
    await screen.findByRole('button', { name: 'Load more options' });
    expect(edited).not.toHaveBeenCalled();
    await fireEvent.click(screen.getByRole('button', { name: 'Load more options' }));
    await screen.findByRole('button', { name: 'Use inventory 2' });
    expect(screen.getByText('Older inventory', { selector: '.badge' })).toBeTruthy();
    expect(screen.getByText(/does not change scope or apply filters/)).toBeTruthy();
    expect(edited).not.toHaveBeenCalled();
  });

  it('keeps retained choices unverified after failure; retry uses the cursor, stale reload does not', async () => {
    connection.client.disks = vi
      .fn()
      .mockResolvedValueOnce(diskPage('1', 'next'))
      .mockRejectedValueOnce(new ApiError('http', 'failed', { status: 500 }))
      .mockRejectedValueOnce(new ApiError('http', 'stale', { status: 409, code: 'stale_cursor' }))
      .mockResolvedValueOnce(diskPage('2'));
    render(ResourcePicker, { value: '1', edited: vi.fn() });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose disk' }));
    await screen.findByRole('button', { name: 'Load more options' });
    await fireEvent.click(screen.getByRole('button', { name: 'Load more options' }));
    await screen.findByRole('button', { name: 'Retry options' });
    expect((screen.getByRole('button', { name: 'Use disk 1' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    await fireEvent.click(screen.getByRole('button', { name: 'Retry options' }));
    await screen.findByText(/Old cursors are retired/);
    expect(screen.queryByRole('button', { name: 'Use disk 1' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Reload options' }));
    await screen.findByRole('button', { name: 'Use disk 2' });
    expect(vi.mocked(connection.client.disks).mock.calls.map(([query]) => query?.cursor)).toEqual([
      undefined,
      'next',
      'next',
      undefined,
    ]);
  });

  it('disconnect and reconnect preserve the draft but require explicit option reload', async () => {
    connection.client.disks = vi.fn().mockResolvedValue(diskPage('1', 'next'));
    connection.client.health = vi.fn().mockResolvedValue({ status: 'ready' });
    connection.client.catalog = vi.fn().mockResolvedValue({
      revision: '3',
      scope: 'current',
      disk_count: '1',
      file_count: '1',
      content_count: '1',
    });
    const edited = vi.fn();
    render(ResourcePicker, { value: '99', edited });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose disk' }));
    await screen.findByRole('button', { name: 'Load more options' });
    connection.report(new ApiError('transport', 'offline'));
    await screen.findByRole('button', { name: 'Retry options' });
    await connection.retry();
    await screen.findByText(/Options invalidated/);
    expect(connection.client.disks).toHaveBeenCalledTimes(1);
    expect((screen.getByRole('textbox', { name: 'Disk ID' }) as HTMLInputElement).value).toBe('99');
    await fireEvent.click(screen.getByRole('button', { name: 'Reload options' }));
    await screen.findByRole('button', { name: 'Use disk 1' });
    expect(vi.mocked(connection.client.disks).mock.calls[1]![0]?.cursor).toBeUndefined();
    expect(edited).not.toHaveBeenCalled();
  });

  it('distinguishes successful empty lists from unavailable resources', async () => {
    connection.client.diskSnapshots = vi
      .fn()
      .mockResolvedValueOnce({ revision: '3', items: [], next_cursor: null })
      .mockRejectedValueOnce(new ApiError('http', 'missing', { status: 404, code: 'not_found' }));
    render(ResourcePicker, { kind: 'inventory', diskID: '1', value: '99', edited: vi.fn() });
    await fireEvent.click(screen.getByRole('button', { name: 'Choose inventory' }));
    await screen.findByText('No complete inventories for this disk.');
    await fireEvent.click(screen.getByRole('button', { name: 'Reload options' }));
    await screen.findByRole('alert');
    expect(screen.queryByText('No complete inventories for this disk.')).toBeNull();
    expect((screen.getByRole('textbox', { name: 'Snapshot ID' }) as HTMLInputElement).value).toBe(
      '99',
    );
  });
});
