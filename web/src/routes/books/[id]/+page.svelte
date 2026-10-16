<script lang="ts">import MemberBook from '../../../components/library/MemberBook.svelte';import type {PageData} from './$types';export let data:PageData;</script>
<svelte:head><title>Book · Home Library</title></svelte:head>
{#key data.id}<MemberBook bookId={Number(data.id)}/>{/key}
