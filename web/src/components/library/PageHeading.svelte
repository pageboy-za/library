<script lang="ts">
export let title: string;
export let description = '';
</script>
<svelte:head><title>{title} · Home Library</title></svelte:head>
<header class="app-heading"><h1>{title}</h1>{#if description}<p>{description}</p>{/if}</header>
