<script lang="ts">
  import type { Result } from '../api/client';
  import { ApiError } from '../api/errors';
  import { useConnection } from '../state/context';
  import { diskPages, snapshotPages } from '../state/feature-requests';
  import RequestFeedback from './RequestFeedback.svelte';
  import type { TraversalState } from '../state/traversal';
  import DateValue from './DateValue.svelte';

  let {
    value = $bindable(''),
    kind = 'disk',
    diskID = '',
    edited,
  }: {
    value: string;
    kind?: 'disk' | 'inventory';
    diskID?: string;
    edited: () => void;
  } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/disks'> | Result<'GET /api/v1/disks/{id}/snapshots'>;
  let open = $state(false);
  let owner = $state<ReturnType<typeof diskPages> | ReturnType<typeof snapshotPages>>();
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let latest = $state<string>();
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  const name = $derived(kind === 'disk' ? 'Disk' : 'Inventory');
  const available = $derived(kind === 'disk' || /^[1-9][0-9]*$/.test(diskID));
  const selected = $derived(items.find((item) => item.id === value));

  $effect(() => {
    if (!open || !available) {
      owner = undefined;
      items = [];
      latest = undefined;
      view = { status: 'idle', pages: [], active: 0, paged: false };
      return;
    }
    const pages =
      kind === 'disk' ? diskPages(connection, 25) : snapshotPages(connection, diskID, 25);
    owner = pages;
    const unsubscribe = pages.traversal.subscribe((state) => {
      view = state;
      items = pages.traversal.items;
      if (!state.pages.length) latest = undefined;
      else if (state.status === 'success' && state.pages[0]!.number === 1)
        latest = state.pages[0]!.value.items[0]?.id;
      paging = {
        busy: pages.traversal.busy,
        canMore: pages.traversal.canMore,
        canNext: pages.traversal.canNext,
        canPrevious: pages.traversal.canPrevious,
      };
    });
    void pages.traversal.restart();
    return () => {
      unsubscribe();
      pages.dispose();
    };
  });

  function choose(id: string) {
    if (value === id) return;
    value = id;
    edited();
  }
  function retry() {
    const traversal = owner?.traversal;
    if (!traversal) return;
    const disconnected =
      view.error instanceof ApiError &&
      (view.error.kind === 'transport' || view.error.status === 503);
    if (!view.pages.length || view.status === 'stale' || disconnected) void traversal.restart();
    else if (view.paged || view.pages.length >= 10) void traversal.next();
    else void traversal.more();
  }
</script>

<div class="resource-picker">
  <div class="control-group">
    <label class="field"
      >{kind === 'disk' ? 'Disk ID' : 'Snapshot ID'}
      <input class="literal" inputmode="numeric" bind:value />
    </label>
    <button type="button" class="button-quiet" aria-expanded={open} onclick={() => (open = !open)}>
      {open ? 'Close' : 'Choose'}
      {kind === 'disk' ? 'disk' : 'inventory'}
    </button>
  </div>
  {#if value}
    {#if selected && view.status === 'success'}
      <div class="muted selected-resource">
        {#if 'label' in selected}<span class="literal">{selected.label}</span> · Disk {selected.id}
        {:else}
          Inventory {selected.id} · <DateValue value={selected.captured_at} meaning="captured_at" />
          {#if latest}<span class="badge"
              >{selected.id === latest ? 'Latest complete inventory' : 'Older inventory'}</span
            >{/if}
        {/if}
      </div>
    {:else}
      <p class="muted">
        {name} ID {value}: unknown / not verified in loaded options. Direct IDs remain usable; not
        loaded does not mean missing.
      </p>
    {/if}
  {/if}
  {#if kind === 'inventory'}
    <p class="muted">
      Choose a disk to browse its complete inventories. Older inventories need History scope to
      search historical observations; choosing one does not change scope or apply filters.
    </p>
  {/if}
  {#if open}
    <section class="options panel" aria-label={`${name} options`}>
      {#if !available}
        <p>
          Enter a valid Disk ID first. A Snapshot ID can still be entered directly without a disk.
        </p>
      {:else}
        <RequestFeedback
          status={view.status}
          error={view.error}
          resource={`${name} options`}
          loadingLabel={`Loading ${name.toLowerCase()} options…`}
          {retry}
          retryLabel="Retry options"
          staleLabel="Inventory changed; reload options. Old cursors are retired."
          staleActionLabel="Reload options"
          invalidatedLabel="Options invalidated; reload explicitly."
          retained={view.pages.length > 0}
          retainedLabel="Options unavailable or not freshly verified; direct IDs are retained."
          emptyLabel={!items.length
            ? kind === 'disk'
              ? 'No disks cataloged.'
              : 'No complete inventories for this disk.'
            : undefined}
        />
        {#if view.status !== 'stale' && (view.status !== 'failure' || view.pages.length > 0)}<button
            type="button"
            disabled={paging.busy}
            onclick={() => void owner?.traversal.restart()}
            >{view.status === 'failure'
              ? 'Reload options from first page'
              : 'Reload options'}</button
          >{/if}
        {#if items.length}
          <ul>
            {#each items as item (item.id)}
              <li>
                <div>
                  {#if 'label' in item}<span class="literal">{item.label}</span><small
                      >Disk {item.id}</small
                    >
                  {:else}
                    <DateValue value={item.captured_at} meaning="captured_at" />
                    <small
                      >Inventory {item.id}{#if latest && view.status === 'success'}
                        · {item.id === latest ? 'Latest complete' : 'Older inventory'}{/if}</small
                    >
                  {/if}
                </div>
                <button
                  type="button"
                  class="button-quiet button-compact"
                  disabled={view.status !== 'success'}
                  aria-label={`Use ${name.toLowerCase()} ${item.id}`}
                  aria-pressed={value === item.id}
                  onclick={() => choose(item.id)}>Use</button
                >
              </li>
            {/each}
          </ul>
        {/if}
        {#if view.pages.length}
          <nav aria-label={`${name} option pages`} class="control-group">
            {#if view.paged}
              <p>Page {view.active}; retained from page {view.pages[0]!.number}.</p>
              <button
                type="button"
                disabled={!paging.canPrevious || paging.busy}
                onclick={() => owner?.traversal.previous()}>Previous options</button
              >
              <button
                type="button"
                disabled={!paging.canNext || paging.busy}
                onclick={() => void owner?.traversal.next()}>Next options</button
              >
            {:else}
              <p>{items.length} options loaded (not catalog total).</p>
              {#if paging.canMore}<button type="button" onclick={() => void owner?.traversal.more()}
                  >Load more options</button
                >
              {:else if paging.canNext}<button
                  type="button"
                  onclick={() => void owner?.traversal.next()}>Next options</button
                >
              {:else if view.pages.at(-1)!.value.next_cursor === null}<p>
                  All options loaded.
                </p>{/if}
            {/if}
          </nav>
        {/if}
      {/if}
    </section>
  {/if}
</div>

<style>
  .resource-picker {
    min-width: 0;
    width: 100%;
  }
  input {
    width: 10rem;
  }
  .options {
    margin-block: var(--space-2);
  }
  ul {
    list-style: none;
    padding: 0;
  }
  li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--space-2);
    padding-block: var(--space-2);
    border-bottom: 1px solid var(--color-border);
  }
  li > div {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  small {
    display: block;
    color: var(--color-muted);
    font-size: var(--text-small);
  }
  .selected-resource {
    margin-block: var(--space-2);
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
</style>
