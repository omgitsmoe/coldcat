<script lang="ts">
  import { page } from '$app/state';
  import Explorer from '../../lib/features/contents/Explorer.svelte';
  import HashLookup from '../../lib/features/contents/HashLookup.svelte';
</script>

{#key page.url.search}<Explorer search={page.url.search} />{/key}
<HashLookup />
