<script lang="ts">
import Borrowers from '../../components/library/Borrowers.svelte';
import ClientOnly from '../../components/library/ClientOnly.svelte';
import PageHeading from '../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Your readers" description="Keep track of the people borrowing your books."/>
<ClientOnly><Borrowers/></ClientOnly>
