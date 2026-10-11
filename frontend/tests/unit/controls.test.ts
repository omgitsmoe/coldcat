import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import PageControls from '../../src/lib/components/PageControls.svelte';
import RequestFeedback from '../../src/lib/components/RequestFeedback.svelte';
import { ApiError } from '../../src/lib/api/errors';
import { CursorTraversal } from '../../src/lib/state/traversal';

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
    expect(
      screen.getByText('Page 15. 500 loaded on this page. Earlier pages retained from page 6.'),
    ).toBeTruthy();
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
  it('keeps action labels stable and disables reload while loading', async () => {
    const reload = vi.fn();
    const view = render(PageControls, { ...props, canMore: true, reload });
    await view.rerender({ busy: true, canMore: false, canNext: false });
    expect((screen.getByRole('button', { name: 'Load more' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(
      (screen.getByRole('button', { name: 'Reload results' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(screen.getByRole('navigation').getAttribute('aria-busy')).toBe('true');
    expect(reload).not.toHaveBeenCalled();
    await view.rerender({ busy: false });
    await fireEvent.click(screen.getByRole('button', { name: 'Reload results' }));
    expect(reload).toHaveBeenCalledOnce();
  });
  it('keeps Next page, not Load more, while entering retained-page navigation', async () => {
    const view = render(PageControls, props);
    const button = screen.getByRole('button', { name: 'Next page' });
    expect(button.classList.contains('page-load')).toBe(false);
    await view.rerender({ busy: true, canNext: false });
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Next page' }).classList.contains('page-load')).toBe(
      false,
    );
  });
  it('separates validation, empty, invalidated and retained failure states without owning requests', async () => {
    const retry = vi.fn();
    const view = render(RequestFeedback, {
      status: 'validation',
      validationLabel: 'Literal <invalid>',
      retry,
      emptyLabel: 'No matching rows.',
      invalidatedLabel: 'Data invalidated; reload explicitly.',
    });
    expect(screen.getByRole('alert').textContent).toBe('Literal <invalid>');
    expect(screen.queryByRole('button')).toBeNull();
    await view.rerender({ status: 'success' });
    expect(screen.getByRole('status').textContent).toBe('No matching rows.');
    await view.rerender({ status: 'idle' });
    expect(screen.getByRole('status').textContent).toContain('reload explicitly');
    expect(retry).not.toHaveBeenCalled();
    await view.rerender({
      status: 'failure',
      retained: true,
      error: new ApiError('http', 'failed', { status: 500 }),
    });
    expect(screen.getByRole('alert').textContent).toContain('not freshly verified');
    expect(screen.queryByText('No matching rows.')).toBeNull();
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(retry).toHaveBeenCalledOnce();
  });
  it('reflects retained controller navigation during suspension and never fetches on presentation changes', async () => {
    const load = vi.fn(async (cursor: string | undefined) => {
      const n = Number(cursor ?? 1);
      return { items: [n], revision: '1', next_cursor: String(n + 1) };
    });
    const traversal = new CursorTraversal({ endpoint: 'disks', resource: '', query: '' }, load, 2);
    await traversal.restart();
    await traversal.more();
    await traversal.next();
    const snapshot = () => ({
      ...props,
      busy: traversal.busy,
      canMore: traversal.canMore,
      canNext: traversal.canNext,
      canPrevious: traversal.canPrevious,
      paged: traversal.state.paged,
      page: traversal.state.active,
      firstRetained: traversal.state.pages[0]!.number,
      loaded: traversal.items.length,
      complete: false,
      previous: () => traversal.previous(),
      next: () => void traversal.next(),
    });
    const view = render(PageControls, snapshot());
    traversal.suspend();
    await view.rerender(snapshot());
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    await fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    await view.rerender(snapshot());
    expect(screen.getByRole('status').textContent).toContain('Page 2. 1 loaded on this page.');
    await fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await view.rerender(snapshot());
    expect(traversal.state.status).toBe('failure');
    expect(load).toHaveBeenCalledTimes(3);
    traversal.dispose();
  });
});
