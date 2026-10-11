<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { diskPages, snapshotPages } from '../../state/feature-requests';
  import { routes, type DetailContext } from '../../state/routes';
  import type { TraversalState } from '../../state/traversal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import DateValue from '../../components/DateValue.svelte';
  import SizeValue from '../../components/SizeValue.svelte';
  import { formatCount } from '../../format/decimal';
  import { directoryBrowseURL } from '../../state/routes';
  let {
    diskID,
    limit = 50,
    latestID,
    verifiedLatest = false,
    context = {},
  }: {
    diskID?: string;
    limit?: number;
    latestID?: string;
    verifiedLatest?: boolean;
    context?: DetailContext;
  } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/disks'> | Result<'GET /api/v1/disks/{id}/snapshots'>;
  let owner = $state<ReturnType<typeof diskPages> | ReturnType<typeof snapshotPages>>();
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  $effect(() => {
    const pages =
      diskID === undefined
        ? diskPages(connection, limit)
        : snapshotPages(connection, diskID, limit);
    owner = pages;
    const unsubscribe = pages.traversal.subscribe((state) => {
      view = state;
      items = pages.traversal.items;
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

<RequestFeedback
  status={view.status}
  error={view.error}
  resource={diskID ? 'Inventory history' : 'Disks'}
  {retry}
  retained={view.pages.length > 0}
  invalidatedLabel="Data invalidated; reload results."
  emptyLabel={items.length === 0
    ? diskID
      ? 'No complete inventories'
      : 'No disks cataloged'
    : undefined}
/>
{#if (view.status === 'idle' || view.status === 'loading') && !view.pages.length}<button
    disabled={paging.busy}
    onclick={() => void owner?.traversal.restart()}>Reload results</button
  >{/if}
{#if view.status === 'success' && items.length === 0}
  <p>Imports are CLI-only. Stop the server before CLI catalog operations, then restart it.</p>
{/if}
{#if items.length}
  <table class="data-table inventory-table">
    <caption>{diskID ? 'Complete inventories · newest capture first' : 'Cataloged disks'}</caption>
    <thead
      ><tr>
        {#if diskID}
          <th scope="col">Inventory</th><th scope="col">Capture</th>
          <th scope="col">Files / contents</th><th scope="col">Actions</th>
        {:else}
          <th scope="col">Disk</th><th scope="col">Declared capacity</th>
          <th scope="col">Latest capture</th><th scope="col">Inventory availability</th>
        {/if}
      </tr></thead
    >
    <tbody>
      {#each items as item (item.id)}
        <tr>
          {#if 'label' in item}
            <td data-label="Disk"
              ><a class="literal" href="{resolve('/')}{routes.disk(item.id, context).slice(1)}"
                >{item.label}</a
              >
              <div class="muted">Disk <span class="literal">{item.id}</span></div></td
            >
            <td data-label="Declared capacity"
              ><SizeValue value={item.capacity} units="decimal" /></td
            >
            <td data-label="Latest capture"
              >{#if item.latest_snapshot}<DateValue
                  value={item.latest_snapshot.captured_at}
                  meaning="captured_at"
                />{:else}—{/if}</td
            >
            <td data-label="Inventory availability">
              {#if item.latest_snapshot}
                <a href="{resolve('/')}{routes.snapshot(item.latest_snapshot.id, context).slice(1)}"
                  >Inventory {item.latest_snapshot.id}</a
                >
                <div class="muted">
                  Latest complete{view.status !== 'success' ? ' · not freshly verified' : ''}
                </div>
                <a
                  class="button button-quiet button-compact"
                  href="{resolve('/')}{directoryBrowseURL(item.latest_snapshot.id, {
                    path: '',
                  }).slice(1)}">Browse root</a
                >
              {:else}No complete inventory{/if}
            </td>
          {:else}
            <td data-label="Inventory"
              ><a href="{resolve('/')}{routes.snapshot(item.id, context).slice(1)}"
                >Inventory {item.id}</a
              >
              {#if verifiedLatest && view.status === 'success'}<div class="badge">
                  {item.id === latestID ? 'Latest complete inventory' : 'Historical inventory'}
                </div>{/if}
            </td>
            <td data-label="Capture"
              ><DateValue value={item.captured_at} meaning="captured_at" /></td
            >
            <td data-label="Files / contents"
              ><span class="numeric"
                >{formatCount(item.file_count)} files · {formatCount(item.content_count)} contents</span
              ></td
            >
            <td data-label="Actions"
              ><a
                class="button button-quiet button-compact"
                href="{resolve('/')}{directoryBrowseURL(item.id, { path: '' }).slice(1)}"
                >Browse root</a
              ></td
            >
          {/if}
        </tr>
      {/each}
    </tbody>
  </table>
{/if}
{#if !(view.status === 'success' && items.length === 0)}
  <p class="muted">
    Imports are CLI-only. Stop the server before CLI catalog operations, then restart it.
  </p>
{/if}
{#if view.pages.length}
  <PageControls
    reload={() => void owner?.traversal.restart()}
    reloadLabel={view.status === 'failure' ? 'Reload from first page' : 'Reload results'}
    {...paging}
    paged={view.paged}
    page={view.active}
    firstRetained={view.pages[0]!.number}
    loaded={items.length}
    complete={view.pages.at(-1)!.value.next_cursor === null}
    more={() => void owner?.traversal.more()}
    next={() => void owner?.traversal.next()}
    previous={() => owner?.traversal.previous()}
  />
{/if}

<style>
  .inventory-table {
    margin-block: var(--space-3);
  }
  @media (max-width: 900px) {
    .inventory-table thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    .inventory-table tbody {
      display: block;
    }
    .inventory-table tr {
      display: grid;
      grid-template-columns: 1fr 1fr;
      border-bottom: 1px solid var(--color-border);
    }
    .inventory-table td {
      display: block;
      min-width: 0;
      height: auto;
      min-height: 40px;
      border: 0;
    }
    .inventory-table td:first-child {
      grid-column: 1 / -1;
    }
    .inventory-table td::before {
      content: attr(data-label);
      display: block;
      color: var(--color-muted);
      font-size: var(--text-small);
    }
  }
</style>
