import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import PageControls from '../../src/lib/components/PageControls.svelte';
import RequestFeedback from '../../src/lib/components/RequestFeedback.svelte';
import { ApiError } from '../../src/lib/api/errors';

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
describe('shared controls and feedback', () => {
  const props = {
    busy: false,
    canMore: false,
    canNext: true,
    canPrevious: false,
    paged: false,
    page: 10,
    firstRetained: 1,
    loaded: 500,
    complete: false,
    more: vi.fn(),
    next: vi.fn(),
    previous: vi.fn(),
  };
  it('makes bounded continuation explicit and does not invent a total', async () => {
    render(PageControls, props);
    expect(screen.getByText('500 loaded')).toBeTruthy();
    expect(screen.getByText(/Retained-page limit/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(props.next).toHaveBeenCalledOnce();
  });
  it('labels limited restoration and disables previous at the retained edge', () => {
    render(PageControls, { ...props, paged: true, page: 15, firstRetained: 6, canNext: false });
    expect(screen.getByText('Page 15. Earlier pages retained from page 6.')).toBeTruthy();
    expect(
      (screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
  });
  it('announces extended loading without an empty result and clears it after success', async () => {
    vi.useFakeTimers();
    const view = render(RequestFeedback, { status: 'loading' });
    expect(screen.getByRole('status').textContent).toBe('Loading…');
    await vi.advanceTimersByTimeAsync(1000);
    expect(screen.getByRole('status').textContent).toContain('Still working');
    await view.rerender({ status: 'success' });
    expect(screen.queryByRole('status')).toBeNull();
  });
  it('renders errors as text, resource not-found and stale reset separately', async () => {
    const view = render(RequestFeedback, {
      status: 'failure',
      resource: 'Disk',
      error: new ApiError('http', '<script>literal</script>', { status: 404 }),
    });
    expect(screen.getByRole('alert').textContent).toContain(
      'Disk not found: <script>literal</script>',
    );
    expect(view.container.querySelector('script')).toBeNull();
    const retry = vi.fn();
    await view.rerender({ status: 'stale', retry });
    await fireEvent.click(screen.getByRole('button', { name: 'Reload results' }));
    expect(retry).toHaveBeenCalledOnce();
  });
  it('never offers blind retry for uncertain writes', () => {
    render(RequestFeedback, {
      status: 'failure',
      error: new ApiError('transport', 'offline', { uncertainWrite: true }),
      retry: vi.fn(),
    });
    expect(screen.getByRole('alert').textContent).toContain('Reload and reconcile');
    expect(screen.queryByRole('button')).toBeNull();
  });
  it('does not call a disconnected traversal complete', () => {
    render(PageControls, { ...props, canNext: false });
    expect(screen.getByText(/Continuation unavailable/)).toBeTruthy();
    expect(screen.queryByText('All results loaded.')).toBeNull();
  });
});
