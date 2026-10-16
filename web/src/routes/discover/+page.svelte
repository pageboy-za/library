<script lang="ts">
import {onMount} from 'svelte';
import {goto} from '$app/navigation';
onMount(()=>{void goto('/staff/discover/'+location.search,{replaceState:true});});
</script>
<h1>Staff area</h1><p>Opening the staff area…</p><a href="/staff/discover/">Continue to staff area</a>
