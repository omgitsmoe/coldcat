<script lang="ts">
  import { page } from '$app/state';
  import { parsePageLimit } from '../../lib/state/routes';
  import InventoryPages from '../../lib/features/disks/InventoryPages.svelte';
  import DiskForm from '../../lib/features/disks/DiskForm.svelte';
  let listVersion = $state(0);
  let creating = $state(false);
  const input = $derived.by(() => {
    try {
      return { limit: parsePageLimit(new globalThis.URLSearchParams(page.url.search)).limit };
    } catch (error) {
      return { error: error instanceof Error ? error.message : 'Invalid disk list route' };
    }
  });
</script>

<div class="actions disk-heading">
  <h1>Disks</h1>
  {#if !input.error && !creating}<button class="button-primary" onclick={() => (creating = true)}
      >Create disk</button
    >{/if}
</div>
{#if input.error}<p role="alert">{input.error}</p>
  <pre>{page.url.search}</pre>
{:else}
  {#key `${page.url.search}:${listVersion}`}<InventoryPages limit={input.limit} />{/key}
  {#if creating}<DiskForm
      closed={() => (creating = false)}
      refreshed={() => {
        listVersion++;
      }}
    />{/if}
{/if}

<style>
  .disk-heading {
    justify-content: space-between;
  }
</style>
