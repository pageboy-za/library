<script lang="ts">
import {onMount} from 'svelte';
import {goto} from '$app/navigation';
onMount(()=>{void goto('/staff/scan/'+location.search,{replaceState:true});});
</script>
<h1>Staff area</h1><p>Opening the staff area…</p><a href="/staff/scan/">Continue to staff area</a>
