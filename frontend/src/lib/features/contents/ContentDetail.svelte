<script lang="ts">
  import { resolve } from '$app/paths';
  import { useConnection } from '../../state/context';
  import { parseContentRoute, contentURL, routes } from '../../state/routes';
  import { contentDetail, locationPages } from '../../state/feature-requests';
  import type { RequestState } from '../../state/request';
  import type { TraversalState } from '../../state/traversal';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { formatBytes, formatCount } from '../../format/decimal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import CopyButton from '../../components/CopyButton.svelte';
  import Locations from './Locations.svelte';
  let { id, search }: { id: string; search: string } = $props();
  const connection = useConnection();
  type Content = Result<'GET /api/v1/contents/{id}'>;
  type Page = Result<'GET /api/v1/contents/{id}/observations'>;
  let route = $state<ReturnType<typeof parseContentRoute>>();
  let parseError = $state('');
  let summary = $state<RequestState<Content>>({ status: 'idle' });
  let cachedSummary = $state<Content>();
  let owner = $state<ReturnType<typeof locationPages>>();
  let detail = $state<ReturnType<typeof contentDetail>>();
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  const content = $derived(summary.status === 'success' ? summary.value : cachedSummary);

  $effect(() => {
    let parsed: ReturnType<typeof parseContentRoute>;
    try {
      parsed = parseContentRoute(new globalThis.URLSearchParams(search));
      route = parsed;
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid content route';
      return;
    }
    const summaryOwner = contentDetail(connection, id, { scope: 'history' });
    const locationsOwner = locationPages(connection, id, parsed.locations);
    detail = summaryOwner;
    owner = locationsOwner;
    const unbindSummary = summaryOwner.request.subscribe((state) => {
      summary = state;
      if (state.status === 'success') cachedSummary = state.value;
    });
    const unbindPages = locationsOwner.traversal.subscribe((state) => {
      view = state;
      items = locationsOwner.traversal.items;
      paging = {
        busy: locationsOwner.traversal.busy,
        canMore: locationsOwner.traversal.canMore,
        canNext: locationsOwner.traversal.canNext,
        canPrevious: locationsOwner.traversal.canPrevious,
      };
    });
    const unbindCache = connection.onInvalidate((reason) => {
      if (reason !== 'disconnect') cachedSummary = undefined;
    });
    void summaryOwner.reload();
    void locationsOwner.traversal.restart();
    return () => {
      unbindSummary();
      unbindPages();
      unbindCache();
      summaryOwner.dispose();
      locationsOwner.dispose();
    };
  });
  function retryLocations() {
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

<h1>Content {id}</h1>
<a href={resolve('/contents')}>Hash lookup</a>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if route}
  {#if route.context.returnTo}<p>
      <a href="{resolve('/')}{route.context.returnTo.slice(1)}"
        >{route.context.returnTo.startsWith('/snapshots/')
          ? 'Return to directory'
          : 'Return to search'}</a
      >
    </p>{/if}
  {#if route.context.observation}<p>
      <a
        href="{resolve('/')}{routes.observation(route.context.observation, route.context).slice(1)}"
        >Selected observation {route.context.observation}</a
      >
      — selection is independent of the location scope and loaded pages.
    </p>{/if}
  <section aria-labelledby="content-identity">
    <h2 id="content-identity">Content identity</h2>
    <RequestFeedback
      status={summary.status}
      error={summary.status === 'failure' ? summary.error : undefined}
      resource="Content"
      retry={() => void detail?.reload()}
    />
    {#if summary.status === 'idle'}<p role="status">
        Content data invalidated; not freshly verified.
        <button onclick={() => void detail?.reload()}>Reload content</button>
      </p>{/if}
    {#if content}
      {#if summary.status !== 'success'}<p>
          Previously loaded identity; not freshly verified.
        </p>{/if}
      <dl>
        <dt>Algorithm</dt>
        <dd>{content.hash.algorithm}</dd>
        <dt>Digest</dt>
        <dd>
          <code class="digest">{content.hash.hex}</code><CopyButton
            text={content.hash.hex}
            label="Copy digest"
          />
        </dd>
        <dt>Size</dt>
        <dd>{formatBytes(content.size)}</dd>
        <dt>Current disks</dt>
        <dd>{formatCount(content.current_disk_count)}</dd>
        <dt>Current locations</dt>
        <dd>{formatCount(content.current_location_count)}</dd>
        <dt>History distinct disks</dt>
        <dd>{formatCount(content.disk_count)}</dd>
        <dt>History distinct disk/path locations</dt>
        <dd>{formatCount(content.location_count)}</dd>
        <dt>Historical observations (all complete inventories)</dt>
        <dd>{formatCount(content.observation_count)}</dd>
      </dl>
      <p>
        Current counts use the latest complete inventory of each disk. Multiple paths on one disk
        are locations, not extra disks.
      </p>
    {/if}
  </section>
  <section aria-labelledby="locations">
    <h2 id="locations">Locations</h2>
    <nav aria-label="Location scope">
      <a
        href="{resolve('/')}{contentURL(
          id,
          { ...route.locations, scope: 'current' },
          route.context,
        ).slice(1)}"
        aria-current={route.locations.scope === 'current' ? 'page' : undefined}>Current locations</a
      >
      <a
        href="{resolve('/')}{contentURL(
          id,
          { ...route.locations, scope: 'history' },
          route.context,
        ).slice(1)}"
        aria-current={route.locations.scope === 'history' ? 'page' : undefined}>History</a
      >
    </nav>
    {#if route.locations.scope === 'history'}<p>
        History includes older inventories; repeated observations are not extra current copies.
      </p>{/if}
    <RequestFeedback
      status={view.status}
      error={view.error}
      resource="Content locations"
      retry={retryLocations}
    />
    {#if view.status === 'idle'}<p>
        Location data invalidated. <button onclick={() => void owner?.traversal.restart()}
          >Reload locations</button
        >
      </p>{/if}
    {#if view.error && view.pages.length}<p role="status">
        Retained locations are not freshly verified.
      </p>{/if}
    {#if view.status === 'success' && !items.length}<p>
        {route.locations.scope === 'history'
          ? 'No historical observations in complete inventories.'
          : 'No current locations in complete inventories.'}
      </p>{/if}
    {#if items.length}<Locations {items} context={route.context} />{/if}
    {#if view.pages.length && owner}
      <PageControls
        busy={paging.busy}
        canMore={paging.canMore}
        canNext={paging.canNext}
        canPrevious={paging.canPrevious}
        paged={view.paged}
        page={view.active}
        firstRetained={view.pages[0]!.number}
        loaded={items.length}
        complete={view.pages.at(-1)!.value.next_cursor === null}
        more={() => void owner?.traversal.more()}
        next={() => void owner?.traversal.next()}
        previous={() => owner?.traversal.previous()}
      />
      <button onclick={() => void owner?.traversal.restart()}>Reload locations</button>
    {/if}
  </section>
{/if}

<style>
  .digest {
    overflow-wrap: anywhere;
    display: block;
  }
  dt {
    font-weight: bold;
  }
  dd {
    margin: 0 0 0.75rem;
  }
  nav a {
    display: inline-block;
    margin: 0.5rem;
  }
</style>
