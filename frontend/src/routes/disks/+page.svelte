<script lang="ts">
  import { page } from '$app/state';
  import { parsePageLimit } from '../../lib/state/routes';
  import InventoryPages from '../../lib/features/disks/InventoryPages.svelte';
  import DiskForm from '../../lib/features/disks/DiskForm.svelte';
  let listVersion = $state(0);
  const input = $derived.by(() => {
    try {
      return { limit: parsePageLimit(new globalThis.URLSearchParams(page.url.search)).limit };
    } catch (error) {
      return { error: error instanceof Error ? error.message : 'Invalid disk list route' };
    }
  });
</script>

<h1>Disks</h1>
{#if input.error}<p role="alert">{input.error}</p>
  <pre>{page.url.search}</pre>
{:else}
  <DiskForm
    refreshed={() => {
      listVersion++;
    }}
  />
  {#key `${page.url.search}:${listVersion}`}<InventoryPages limit={input.limit} />{/key}
{/if}
