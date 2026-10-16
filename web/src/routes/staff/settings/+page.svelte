<script lang="ts">
import BackupStatus from '../../../components/library/BackupStatus.svelte';
import Updates from '../../../components/library/Updates.svelte';
import DeletedBooks from '../../../components/library/DeletedBooks.svelte';
import DefaultShelf from '../../../components/library/DefaultShelf.svelte';
import PageHeading from '../../../components/library/PageHeading.svelte';
</script>
<PageHeading title="Settings" description="Choose your default shelf, check backups and install approved updates."/>
<DefaultShelf/><BackupStatus/><Updates/><DeletedBooks/>
