import { cleanup, render, screen, fireEvent } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import Shell from '../../src/lib/components/Shell.svelte';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('bootstrap shell readiness', () => {
  it('has landmarks and reports readiness without catalog requests', async () => {
    const request = vi.fn().mockResolvedValue(new Response('{"status":"ready"}'));
    vi.stubGlobal('fetch', request);
    render(Shell);
    expect(screen.getByRole('main')).toBeTruthy();
    expect(await screen.findByText('Backend ready')).toBeTruthy();
    expect(request).toHaveBeenCalledExactlyOnceWith('/healthz');
  });

  it('reports outage, then retries only when asked', async () => {
    const request = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(new Response('{"status":"ready"}'));
    vi.stubGlobal('fetch', request);
    render(Shell);
    expect(
      await screen.findByText('Backend unavailable. Start the local server, then retry.'),
    ).toBeTruthy();
    await fireEvent.click(screen.getByRole('button', { name: 'Retry connection' }));
    expect(await screen.findByText('Backend ready')).toBeTruthy();
    expect(request).toHaveBeenCalledTimes(2);
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
