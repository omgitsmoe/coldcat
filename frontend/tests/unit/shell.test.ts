import { cleanup, render, screen, fireEvent } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Shell from '../../src/lib/components/Shell.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const json = (value: unknown) =>
  new Response(JSON.stringify(value), {
    headers: { 'Content-Type': 'application/json' },
  });
const catalog = {
  revision: '0',
  scope: 'current',
  disk_count: '0',
  file_count: '0',
  content_count: '0',
};

describe('shell connection', () => {
  it('has landmarks and catalog-backed readiness', async () => {
    const request = vi
      .fn()
      .mockResolvedValueOnce(json({ status: 'ready' }))
      .mockResolvedValueOnce(json(catalog));
    vi.stubGlobal('fetch', request);
    render(Shell);
    expect(screen.getByRole('main')).toBeTruthy();
    expect(await screen.findByText('Backend ready')).toBeTruthy();
    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls.map(([url]) => url)).toEqual(['/healthz', '/api/v1/catalog']);
  });

  it('reports outage, then retries only when asked', async () => {
    const request = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(json({ status: 'ready' }))
      .mockResolvedValueOnce(json(catalog));
    vi.stubGlobal('fetch', request);
    render(Shell);
    expect(
      await screen.findByText('Backend unavailable. Start the local server, then retry.'),
    ).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Retry connection' }));
    expect(await screen.findByText('Backend ready')).toBeTruthy();
    expect(request).toHaveBeenCalledTimes(3);
  });

  it.each([
    new Response('unavailable', { status: 503 }),
    new Response('<html>not JSON</html>'),
    new Response('{"status":"not-ready"}'),
  ])('never treats an error or unexpected response as ready', async (response) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response));
    render(Shell);
    expect(
      await screen.findByText('Backend unavailable. Start the local server, then retry.'),
    ).toBeTruthy();
  });
});
