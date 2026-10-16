<script lang="ts">
import {onMount} from 'svelte';
import {goto} from '$app/navigation';
onMount(()=>{void goto('/staff/settings/'+location.search,{replaceState:true});});
</script>
<h1>Staff area</h1><p>Opening the staff area…</p><a href="/staff/settings/">Continue to staff area</a>
<!-- Runtime readiness marker; settings require administrator sign-in. -->
<h2>Application updates</h2>
