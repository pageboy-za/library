<script lang="ts">
import LiveKiosk from '../components/library/LiveKiosk.svelte';
import ClientOnly from '../components/library/ClientOnly.svelte';
</script>
<svelte:head><title>Your library</title></svelte:head>
<ClientOnly><LiveKiosk/></ClientOnly>
