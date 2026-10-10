<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { contentsPages } from '../../state/feature-requests';
  import { parseContents, routes, type ContentsRoute } from '../../state/routes';
  import type { TraversalState } from '../../state/traversal';
  import { formatBytes, formatCount, otherCount } from '../../format/decimal';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import Filters from './Filters.svelte';
  let { search }: { search: string } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/contents'>;
  let query = $state<ContentsRoute>();
  let parseError = $state('');
  let owner = $state<ReturnType<typeof contentsPages>>();
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let draft = $state(false);
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  $effect(() => {
    let input: ContentsRoute;
    try {
      input = parseContents(new globalThis.URLSearchParams(search));
    } catch (error) {
      parseError = error instanceof Error ? error.message : 'Invalid content filters';
      return;
    }
    query = input;
    const pages = contentsPages(connection, input);
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
  function retire() {
    draft = true;
    owner?.traversal.retire();
  }
  function resume() {
    draft = false;
    void owner?.traversal.restart();
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

<h1>Distinct contents</h1>
<p>
  One row per algorithm/hash identity, not per filename or observation. Aliases and repeated
  inventories do not create extra rows.
</p>
<p>
  Counts are catalog-wide despite disk/directory membership filters. Other counts subtract one from
  nonzero current content counts, not from a selected historical observation.
</p>
{#if parseError}<p role="alert">{parseError}</p>
  <pre>{search}</pre>
{:else if query}
  <Filters {query} {retire} {resume} />
  {#if draft}<p role="status">Draft filters; apply to load results.</p>
  {:else}
    <p>Applied scope: {query.scope}; metric: Other {query.replica_metric}.</p>
    <RequestFeedback status={view.status} error={view.error} resource="Contents" {retry} />
    {#if view.status !== 'stale'}<button disabled={paging.busy} onclick={resume}
        >Reload results</button
      >{/if}
    {#if view.status === 'idle'}<p role="status">Data invalidated; reload results.</p>{/if}
    {#if view.error && view.pages.length}<p>
        Previously loaded results; not freshly verified.
      </p>{/if}
    {#if view.status === 'success' && !items.length}<p>No matching distinct contents.</p>{/if}
  {/if}
  <table>
    <caption>Distinct content identities — {query.scope} membership</caption>
    <thead
      ><tr
        ><th>Content / hash</th><th>Size</th><th>Current catalog copies</th><th
          >Scoped history / observations</th
        ></tr
      ></thead
    >
    <tbody
      >{#each items as item (item.id)}
        <tr
          ><td
            ><a href="{resolve('/')}{routes.content(item.id).slice(1)}">Content {item.id}</a>
            <div class="literal">{item.hash.algorithm}: {item.hash.hex}</div></td
          >
          <td>{formatBytes(item.size)}</td>
          <td
            ><div>{formatCount(item.current_disk_count)} current disks</div>
            <div>{formatCount(item.current_location_count)} current locations</div>
            <strong
              >{formatCount(
                otherCount(
                  query.replica_metric === 'locations'
                    ? item.current_location_count
                    : item.current_disk_count,
                ),
              )} other {query.replica_metric}</strong
            ></td
          >
          <td
            ><div>{formatCount(item.disk_count)} {item.scope} distinct disks</div>
            <div>{formatCount(item.location_count)} {item.scope} distinct disk/path locations</div>
            <div>
              {formatCount(item.observation_count)} observations across all complete inventories
            </div></td
          ></tr
        >
      {/each}</tbody
    >
  </table>
  {#if !draft && view.pages.length}
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
{/if}

<style>
  table {
    width: 100%;
    border-collapse: collapse;
  }
  caption,
  th,
  td {
    text-align: left;
  }
  th,
  td {
    padding: 0.5rem;
    vertical-align: top;
    border-bottom: 1px solid #777;
  }
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 700px) {
    table,
    tbody,
    tr,
    td {
      display: block;
    }
    thead {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
    tr {
      border: 1px solid #777;
      margin-block: 1rem;
    }
    td {
      border: 0;
    }
  }
</style>
