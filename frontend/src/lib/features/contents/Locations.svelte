<script lang="ts">
  import { resolve } from '$app/paths';
  import type { Result } from '../../api/client';
  import { routes, directoryBrowseURL, type DetailContext } from '../../state/routes';
  import { containingDirectory } from '../../format/path';
  import { formatBytes } from '../../format/decimal';
  import DateValue from '../../components/DateValue.svelte';
  let {
    items,
    context,
  }: {
    items: Result<'GET /api/v1/contents/{id}/observations'>['items'];
    context: DetailContext;
  } = $props();
</script>

<table>
  <caption>Observation locations — inventory history is not a count of current copies</caption>
  <thead><tr><th>Path</th><th>Disk / inventory</th><th>Size / dates</th><th>Actions</th></tr></thead
  >
  <tbody>
    {#each items as item (item.observation.id)}
      <tr aria-current={context.observation === item.observation.id ? 'true' : undefined}>
        <td
          ><div class="literal">{item.observation.path}</div>
          <div>{item.is_current ? 'Current' : 'Historical'}</div>
          {#if context.observation === item.observation.id}<strong>Selected</strong>{/if}
        </td>
        <td
          ><a href="{resolve('/')}{routes.disk(item.disk.id, context).slice(1)}"
            >{item.disk.label}</a
          >
          <br /><a href="{resolve('/')}{routes.snapshot(item.snapshot.id, context).slice(1)}"
            >Inventory {item.snapshot.id}</a
          ></td
        >
        <td
          >{formatBytes(item.size)}
          <DateValue value={item.snapshot.captured_at} meaning="captured_at" />
          <DateValue value={item.observation.mtime} meaning="mtime" /></td
        >
        <td
          ><a href="{resolve('/')}{routes.observation(item.observation.id, context).slice(1)}"
            >Observation {item.observation.id}</a
          >
          <br /><a
            href="{resolve('/')}{directoryBrowseURL(item.snapshot.id, {
              path: containingDirectory(item.observation.path),
            }).slice(1)}">Containing directory</a
          ></td
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
  tr[aria-current='true'] {
    outline: 2px solid currentColor;
    outline-offset: -2px;
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
