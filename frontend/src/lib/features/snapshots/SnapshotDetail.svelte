<script lang="ts">
  import { resolve } from '$app/paths';
  import { useConnection } from '../../state/context';
  import { snapshotDetail } from '../../state/feature-requests';
  import { parseDetailContext, routes } from '../../state/routes';
  import type { RequestState } from '../../state/request';
  import type { Result } from '../../api/client';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import InventorySummary from './InventorySummary.svelte';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Snapshot = Result<'GET /api/v1/snapshots/{id}'>;
  let context = $state<ReturnType<typeof parseDetailContext>>();
  let parseError = $state('');
  let owner = $state<ReturnType<typeof snapshotDetail>>();
  let view = $state<RequestState<Snapshot>>({ status: 'idle' });
  let cached = $state<Snapshot>();
  const item = $derived(view.status === 'success' ? view.value : cached);
  $effect(() => {
    try {
      context = parseDetailContext(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid inventory route';
      return;
    }
    const detail = snapshotDetail(connection, id);
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

<h1>Inventory {id}</h1>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if context}
  {#if context.returnTo}<p>
      <a href="{resolve('/')}{context.returnTo.slice(1)}">Return to search</a>
    </p>{/if}
  <RequestFeedback
    status={view.status}
    error={view.status === 'failure' ? view.error : undefined}
    resource="Inventory"
    retry={() => void owner?.reload()}
  />
  <button onclick={() => void owner?.reload()}>Reload inventory</button>
  {#if view.status === 'idle'}<p>Inventory data invalidated; not freshly verified.</p>{/if}
  {#if item}
    {#if view.status !== 'success'}<p>Previously loaded inventory; not freshly verified.</p>{/if}
    <p>
      <a href="{resolve('/')}{routes.disk(item.disk_id, context).slice(1)}">Disk {item.disk_id}</a>
    </p>
    <InventorySummary {item} {context} provenance />
    <p>
      Captured at describes the source inventory's capture time; Imported at is when the catalog
      received it. Capture age is not proof an offline disk was recently checked.
    </p>
    <p>Open the disk to see its latest complete inventory and capture-ordered history.</p>
  {/if}
{/if}
