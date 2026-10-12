<script lang="ts">
  import { resolve } from '$app/paths';
  import type { components } from '../../api/generated/wire';
  import { contentURL, directoryBrowseURL, routes, type DirectoryRoute } from '../../state/routes';
  import { formatCount } from '../../format/decimal';
  import SizeValue from '../../components/SizeValue.svelte';
  import DateValue from '../../components/DateValue.svelte';
  let {
    items,
    id,
    query,
    returnTo,
    searchReturnTo,
  }: {
    items: components['schemas']['DirectoryEntry'][];
    id: string;
    query: DirectoryRoute;
    returnTo: string;
    searchReturnTo?: string;
  } = $props();
</script>

<table class="data-table">
  <caption>Directory entries</caption>
  <thead
    ><tr
      ><th>Path / kind</th><th class="numeric">Size</th><th class="numeric"
        >Current other replicas</th
      ><th>Actions</th></tr
    ></thead
  >
  <tbody>
    {#each items as item (`${item.kind}:${item.path}`)}
      <tr>
        <td class="path">
          {#if item.directory}
            <a
              class="literal"
              href="{resolve('/')}{directoryBrowseURL(
                id,
                { ...query, path: item.path },
                { returnTo: searchReturnTo },
              ).slice(1)}">{item.path}</a
            >
            <div>Directory</div>
          {:else}<span class="literal">{item.path}</span>
            <div>File</div>{/if}
        </td>
        <td class="numeric">
          <span class="narrow-label">Size</span>
          {#if item.directory}
            Recursive file bytes: <SizeValue
              value={item.directory.size_complete ? item.directory.known_bytes : null}
            />
            <div>
              Known subtotal: <SizeValue value={item.directory.known_bytes} />; unknown files: {formatCount(
                item.directory.unknown_size_file_count,
              )}
            </div>
          {:else if item.file}
            <SizeValue value={item.file.size} />
            <DateValue value={item.file.observation.mtime} meaning="mtime" />
          {/if}
        </td>
        <td class="numeric"
          ><span class="narrow-label">Current other replicas</span>{#if item.file}{formatCount(
              item.file.other_disk_count,
            )} other disks<br />{formatCount(item.file.other_location_count)} other locations{:else}Navigation
            entry (not filtered by file replica bounds){/if}</td
        >
        <td
          ><span class="narrow-label">Actions</span>{#if item.file}
            <a
              href="{resolve('/')}{contentURL(
                item.file.observation.content_id,
                { scope: 'current' },
                { returnTo, observation: item.file.observation.id },
              ).slice(1)}">Content {item.file.observation.content_id}</a
            ><br />
            <a
              href="{resolve('/')}{routes
                .observation(item.file.observation.id, {
                  returnTo,
                  observation: item.file.observation.id,
                })
                .slice(1)}">Observation {item.file.observation.id}</a
            >
          {/if}</td
        >
      </tr>
    {/each}
  </tbody>
</table>

<style>
  th:first-child {
    width: 44%;
  }
  th:nth-child(2) {
    width: 23%;
  }
  th:nth-child(3) {
    width: 18%;
  }
  th:last-child {
    width: 15%;
  }
  .narrow-label {
    display: none;
  }
  .literal {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 900px) {
    table,
    tbody,
    tr,
    td {
      display: block;
    }
    caption {
      display: block;
      width: 100%;
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
      padding-block: var(--space-2);
    }
    td {
      border: 0;
      height: auto;
      min-height: 0;
      text-align: left;
      padding-block: var(--space-1);
    }
    .data-table td.numeric {
      text-align: left;
    }
    .narrow-label {
      display: block;
      font-weight: 600;
      color: var(--color-muted);
    }
  }
</style>
