<script lang="ts">
import { onMount, type Snippet } from 'svelte';
let { children }: { children: Snippet } = $props();
let mounted = $state(false);
onMount(() => { mounted = true; });
</script>
{#if mounted}{@render children()}{:else}<p role="status">Loading…</p>{/if}
