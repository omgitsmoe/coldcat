<script lang="ts">
  import { resolve } from '$app/paths';
  import { useConnection } from '../../state/context';
  import { diskDetail } from '../../state/feature-requests';
  import { parseInventoryRoute } from '../../state/routes';
  import type { RequestState } from '../../state/request';
  import type { Result } from '../../api/client';
  import { formatBytes, formatCount } from '../../format/decimal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import DiskMetadata from './DiskMetadata.svelte';
  import InventoryPages from './InventoryPages.svelte';
  import InventorySummary from '../snapshots/InventorySummary.svelte';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Disk = Result<'GET /api/v1/disks/{id}'>;
  let route = $state<ReturnType<typeof parseInventoryRoute>>();
  let parseError = $state('');
  let owner = $state<ReturnType<typeof diskDetail>>();
  let view = $state<RequestState<Disk>>({ status: 'idle' });
  let cached = $state<Disk>();
  const item = $derived(view.status === 'success' ? view.value : cached);
  $effect(() => {
    try {
      route = parseInventoryRoute(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid disk route';
      return;
    }
    const detail = diskDetail(connection, id);
    owner = detail;
    const unsubscribe = detail.request.subscribe((state) => {
      view = state;
      if (state.status === 'success') cached = state.value;
    });
    const unbind = connection.onInvalidate((reason) => {
      if (reason !== 'disconnect') cached = undefined;
    });
    void detail.reload();
    return () => {
      unsubscribe();
      unbind();
      detail.dispose();
    };
  });
</script>

<h1>Disk {id}</h1>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if route}
  {#if route.context.returnTo}<p>
      <a href="{resolve('/')}{route.context.returnTo.slice(1)}">Return to search</a>
    </p>{/if}
  <section aria-labelledby="disk-metadata">
    <h2 id="disk-metadata">Disk metadata</h2>
    <RequestFeedback
      status={view.status}
      error={view.status === 'failure' ? view.error : undefined}
      resource="Disk"
      retry={() => void owner?.reload()}
    />
    <button onclick={() => void owner?.reload()}>Reload disk</button>
    {#if view.status === 'idle'}<p>Disk data invalidated; not freshly verified.</p>{/if}
    {#if item}
      {#if view.status !== 'success'}<p>Previously loaded disk; not freshly verified.</p>{/if}
      <DiskMetadata {item} />
    {/if}
  </section>
  {#if item}
    <section aria-labelledby="cataloged-size">
      <h2 id="cataloged-size">Cataloged inventory size</h2>
      <p>
        Inventory subtotals sum known sizes per file path, including repeated content. They are not
        measured disk usage or free space.
      </p>
      {#if item.cataloged}
        <dl>
          <dt>Known cataloged bytes</dt>
          <dd>{formatBytes(item.cataloged.known_bytes)}</dd>
          <dt>Cataloged total</dt>
          <dd>
            {item.cataloged.size_complete ? formatBytes(item.cataloged.known_bytes) : 'Unknown'}
          </dd>
          <dt>Size completeness</dt>
          <dd>{item.cataloged.size_complete ? 'Complete' : 'Incomplete'}</dd>
          <dt>Files with unknown size</dt>
          <dd>{formatCount(item.cataloged.unknown_size_file_count)}</dd>
          <dt>Files</dt>
          <dd>{formatCount(item.cataloged.file_count)}</dd>
          <dt>Distinct contents</dt>
          <dd>{formatCount(item.cataloged.content_count)}</dd>
        </dl>
      {:else}<p>Cataloged total: Unknown — no complete inventory.</p>{/if}
    </section>
    <section aria-labelledby="latest-inventory">
      <h2 id="latest-inventory">Latest complete inventory</h2>
      <p>
        Latest is selected by capture time, then inventory ID, not import time. Capture age is not
        proof an offline disk was recently checked.
      </p>
      {#if item.latest_snapshot}<InventorySummary
          item={item.latest_snapshot}
          context={route.context}
        />
      {:else}<p>No complete inventory</p>
        <p>
          Imports are CLI-only. Stop the server before CLI catalog operations, then restart it.
        </p>{/if}
    </section>
  {/if}
  <section aria-labelledby="inventory-history">
    <h2 id="inventory-history">Inventory history</h2>
    <p>
      Complete inventories in capture-time order, newest first. Repeated inventories are not extra
      current copies.
    </p>
    <InventoryPages
      diskID={id}
      limit={route.limit}
      context={route.context}
      latestID={item?.latest_snapshot?.id}
      verifiedLatest={view.status === 'success'}
    />
  </section>
{/if}
