<script lang="ts">
import ScanBook from '../../../components/library/ScanBook.svelte';
import ClientOnly from '../../../components/library/ClientOnly.svelte';
import PageHeading from '../../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Add a book" description="Scan an ISBN or add details manually."/>
<ClientOnly><ScanBook/></ClientOnly>
