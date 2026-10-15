<script lang="ts">
  import '../app.css'
  import { page } from '$app/state'

  let { children } = $props()
</script>

<!-- Re-create everything below when the shop changes, so each shop gets its own cart. -->
{#key page.params.slug}
  {@render children()}
{/key}
