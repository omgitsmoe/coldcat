<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { ApiError } from '../../api/errors';
  import { useConnection } from '../../state/context';
  import { contentsPages } from '../../state/feature-requests';
  import { parseContents, routes, type ContentsRoute } from '../../state/routes';
  import type { TraversalState } from '../../state/traversal';
  import { formatCount, otherCount } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  import RequestFeedback from '../../components/RequestFeedback.svelte';
  import PageControls from '../../components/PageControls.svelte';
  import Filters from './Filters.svelte';
  import { contentSummary } from './filters';
  import { parseMembershipDraft } from './membership-draft';
  let { search }: { search: string } = $props();
  const connection = useConnection();
  type Page = Result<'GET /api/v1/contents'>;
  let query = $state<ContentsRoute>();
  let parseError = $state('');
  let owner = $state<ReturnType<typeof contentsPages>>();
  let view = $state<TraversalState<Page>>({ status: 'idle', pages: [], active: 0, paged: false });
  let items = $state<Page['items']>([]);
  let draft = $state(false);
  let bookmarkedDraft = $state(false);
  let paging = $state({ busy: false, canMore: false, canNext: false, canPrevious: false });
  $effect(() => {
    let input: ContentsRoute;
    try {
      input = parseContents(new globalThis.URLSearchParams(search));
    } catch (error) {
      try {
        query = parseMembershipDraft(new globalThis.URLSearchParams(search));
        draft = true;
        bookmarkedDraft = true;
      } catch {
        parseError = error instanceof Error ? error.message : 'Invalid content filters';
      }
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
  <Filters {query} {retire} {resume} {bookmarkedDraft} />
  <p class="literal muted">
    {bookmarkedDraft ? 'Bookmarked draft (not applied)' : draft ? 'Last applied' : 'Applied'}: {contentSummary(
      query,
    )}
  </p>
  {#if draft}<p role="status" class="status-warning">
      Unapplied changes — Draft filters; apply to load results.
    </p>
  {:else}
    <RequestFeedback
      status={view.status}
      error={view.error}
      resource="Contents"
      {retry}
      retained={view.pages.length > 0}
      invalidatedLabel="Data invalidated; reload results."
      emptyLabel={!items.length ? 'No matching distinct contents.' : undefined}
    />
    {#if view.status === 'idle' || view.status === 'loading'}<button
        disabled={paging.busy}
        onclick={resume}>Reload results</button
      >{/if}
  {/if}
  <table class="data-table">
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
          <td class="numeric"
            ><span class="record-label">Size</span><SizeValue value={item.size} /></td
          >
          <td class="numeric"
            ><span class="record-label">Current catalog copies</span>
            <div>{formatCount(item.current_disk_count)} current disks</div>
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
          <td class="numeric"
            ><span class="record-label">Scoped history / observations</span>
            <div>{formatCount(item.disk_count)} {item.scope} distinct disks</div>
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
      reload={resume}
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
{/if}

<style>
  th:first-child {
    width: 40%;
  }
  .record-label {
    display: none;
  }
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 900px) {
    table,
    caption,
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
      border-bottom: 1px solid var(--color-border);
    }
    td {
      border: 0;
      height: auto;
      text-align: left;
    }
    table.data-table td.numeric {
      text-align: left;
    }
    .record-label {
      display: block;
      font-weight: 600;
      color: var(--color-muted);
    }
  }
</style>
