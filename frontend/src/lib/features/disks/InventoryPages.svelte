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
  import DiskMetadata from './DiskMetadata.svelte';
  import InventorySummary from '../snapshots/InventorySummary.svelte';
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
/>
{#if view.status !== 'stale'}<button
    disabled={paging.busy}
    onclick={() => void owner?.traversal.restart()}>Reload results</button
  >{/if}
{#if view.status === 'idle'}<p role="status">Data invalidated; reload results.</p>{/if}
{#if view.error && view.pages.length}<p>Previously loaded results; not freshly verified.</p>{/if}
{#if view.status === 'success' && items.length === 0}
  <p>{diskID ? 'No complete inventories' : 'No disks cataloged'}</p>
  <p>Imports are CLI-only. Stop the server before CLI catalog operations, then restart it.</p>
{/if}
{#each items as item (item.id)}
  <article>
    {#if 'label' in item}
      <h2><a href="{resolve('/')}{routes.disk(item.id, context).slice(1)}">{item.label}</a></h2>
      <DiskMetadata {item} />
      {#if item.latest_snapshot}
        <p>Latest complete inventory (by capture time)</p>
        <InventorySummary item={item.latest_snapshot} {context} />
      {:else}
        <p>No complete inventory</p>
        <p>Imports are CLI-only. Stop the server before CLI catalog operations, then restart it.</p>
      {/if}
    {:else}
      {#if verifiedLatest && !view.error}
        <p>{item.id === latestID ? 'Latest complete inventory' : 'Historical inventory'}</p>
      {/if}
      <InventorySummary {item} {context} />
    {/if}
  </article>
{/each}
{#if view.pages.length}
  <PageControls
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
  article {
    border: 1px solid #777;
    padding: 1rem;
    margin-block: 1rem;
  }
</style>
