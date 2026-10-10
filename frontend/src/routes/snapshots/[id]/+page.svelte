<script lang="ts">
  import { page } from '$app/state';
  import SnapshotDetail from '../../../lib/features/snapshots/SnapshotDetail.svelte';
</script>

{#key page.params.id + page.url.search}
  <SnapshotDetail id={page.params.id!} search={page.url.search} />
{/key}
