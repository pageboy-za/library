<script lang="ts">
import {onMount} from 'svelte';import {api,type Shelf} from '../../lib/api';
type Preferences={default_shelf_id:number|null;revision:number};
let shelves:Shelf[]=[];let preference:Preferences|null=null;let shelf:number|''='';let busy=false;let error='';let message='';
async function load(){try{[shelves,preference]=await Promise.all([api<Shelf[]>('/api/shelves'),api<Preferences>('/api/preferences')]);shelf=preference.default_shelf_id||'';}catch(e){error=(e as Error).message;}}
async function save(event:SubmitEvent){event.preventDefault();if(!preference||busy)return;busy=true;error='';message='';try{preference=await api<Preferences>('/api/preferences',{default_shelf_id:shelf?Number(shelf):null,revision:preference.revision},'PATCH');message='Default shelf saved.';}catch(e){error=(e as Error).message;}finally{busy=false;}}
onMount(load);
</script>
<section class="default-shelf"><h2>Default shelf</h2><p>New books start on this shelf. You can choose another shelf when saving a book.</p><form onsubmit={save}><label class="library-field">Shelf<select bind:value={shelf} disabled={busy||!preference}><option value="">Unassigned</option>{#each shelves as s}<option value={s.id}>{s.bookcase} / {s.name}</option>{/each}</select></label><button class="library-button" disabled={busy||!preference}>{busy?'Saving…':'Save default shelf'}</button></form>{#if error}<p role="alert">{error} <button class="library-button secondary" onclick={load}>Reload settings</button></p>{/if}<p role="status">{message}</p></section>
<style>@layer components{.default-shelf h2{font-family:Georgia,serif;font-size:var(--h4)}.default-shelf{display:grid;gap:1rem;max-width:44rem;margin-top:2rem}.default-shelf form{display:flex;align-items:end;gap:1rem;flex-wrap:wrap}.default-shelf label{flex:1;min-width:12rem}}</style>
