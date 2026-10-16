<script lang="ts">import { page } from '$app/state';</script>
<svelte:head><title>Page unavailable · Home Library</title></svelte:head>
<h1>{page.status === 404 ? 'Page not found' : 'Could not open this page'}</h1>
<p>{page.error?.message || 'Please try again.'}</p><a href="/">Back to library</a>
