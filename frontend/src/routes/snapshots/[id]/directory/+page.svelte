<script lang="ts">
  import { page } from '$app/state';
  import Browse from '../../../../lib/features/directories/Browse.svelte';
</script>

{#key page.params.id + page.url.search}
  <Browse id={page.params.id!} search={page.url.search} />
{/key}
