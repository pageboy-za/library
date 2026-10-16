<script lang="ts">
import LiveBook from '../../../components/library/LiveBook.svelte';
import type { PageData } from './$types';
export let data: PageData;
</script>
{#key data.id}<LiveBook/>{/key}
