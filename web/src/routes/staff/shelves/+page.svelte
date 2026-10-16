<script lang="ts">
import Shelves from '../../../components/library/Shelves.svelte';
import ClientOnly from '../../../components/library/ClientOnly.svelte';
import PageHeading from '../../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Your shelves" description="Name your bookcases and shelves so you can find a book when you need it."/>
<ClientOnly><Shelves/></ClientOnly>
