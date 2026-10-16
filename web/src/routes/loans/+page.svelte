<script lang="ts">
import Loans from '../../components/library/Loans.svelte';
import ClientOnly from '../../components/library/ClientOnly.svelte';
import PageHeading from '../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Loans & returns" description="See who has your books, when they are due, and record returns."/>
<ClientOnly><Loans/></ClientOnly>
