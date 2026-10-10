<script lang="ts">
  import { page } from '$app/state';
  import DiskDetail from '../../../lib/features/disks/DiskDetail.svelte';
</script>

{#key page.params.id + page.url.search}
  <DiskDetail id={page.params.id!} search={page.url.search} />
{/key}
