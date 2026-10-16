<script lang="ts">
import LiveBook from '../../../../components/library/LiveBook.svelte';
import type {PageData} from './$types';
export let data:PageData;
</script>
<svelte:head><title>Book · Staff library</title></svelte:head>
{#key data.id}<LiveBook bookId={Number(data.id)}/>{/key}
