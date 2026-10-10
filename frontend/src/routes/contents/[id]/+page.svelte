<script lang="ts">
  import { page } from '$app/state';
  import ContentDetail from '../../../lib/features/contents/ContentDetail.svelte';
</script>

{#key page.params.id + page.url.search}
  <ContentDetail id={page.params.id!} search={page.url.search} />
{/key}
