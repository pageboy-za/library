<script lang="ts">
import { onMount } from 'svelte';
import { goto } from '$app/navigation';
onMount(() => {
 const id = new URLSearchParams(location.search).get('id');
 void goto(id && /^[1-9][0-9]*$/.test(id) ? `/books/${id}/` : '/', { replaceState: true });
});
</script>
<svelte:head><title>Opening your book · Home Library</title></svelte:head>
<p role="status">Opening your book…</p><a href="/">Back to library</a>
