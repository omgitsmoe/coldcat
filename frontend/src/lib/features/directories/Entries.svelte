<script lang="ts">
  import { resolve } from '$app/paths';
  import type { components } from '../../api/generated/wire';
  import { contentURL, directoryBrowseURL, routes, type DirectoryRoute } from '../../state/routes';
  import { formatBytes, formatCount } from '../../format/decimal';
  import DateValue from '../../components/DateValue.svelte';
  let {
    items,
    id,
    query,
    returnTo,
  }: {
    items: components['schemas']['DirectoryEntry'][];
    id: string;
    query: DirectoryRoute;
    returnTo: string;
  } = $props();
</script>

<table>
  <caption>Directory entries</caption>
  <thead
    ><tr><th>Path / kind</th><th>Size</th><th>Current other replicas</th><th>Actions</th></tr
    ></thead
  >
  <tbody>
    {#each items as item (`${item.kind}:${item.path}`)}
      <tr>
        <td>
          {#if item.directory}
            <a
              class="literal"
              href="{resolve('/')}{directoryBrowseURL(id, { ...query, path: item.path }).slice(1)}"
              >{item.path}</a
            >
            <div>Directory</div>
          {:else}<span class="literal">{item.path}</span>
            <div>File</div>{/if}
        </td>
        <td>
          {#if item.directory}
            Recursive file bytes: {formatBytes(
              item.directory.size_complete ? item.directory.known_bytes : null,
            )}
            <div>
              Known subtotal: {formatBytes(item.directory.known_bytes)}; unknown files: {formatCount(
                item.directory.unknown_size_file_count,
              )}
            </div>
          {:else if item.file}
            {formatBytes(item.file.size)}
            <DateValue value={item.file.observation.mtime} meaning="mtime" />
          {/if}
        </td>
        <td
          >{#if item.file}{formatCount(item.file.other_disk_count)} other disks<br />{formatCount(
              item.file.other_location_count,
            )} other locations{:else}Navigation entry (not filtered by file replica bounds){/if}</td
        >
        <td
          >{#if item.file}
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
