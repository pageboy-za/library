<script lang="ts">
import Discovery from '../../../components/library/Discovery.svelte';
import ClientOnly from '../../../components/library/ClientOnly.svelte';
import PageHeading from '../../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Explore your shelves" description="Find books connected to the ideas you want to explore."/>
<ClientOnly><Discovery/></ClientOnly>
