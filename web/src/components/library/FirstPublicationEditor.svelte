<script lang="ts">
import {api,type Book} from '../../lib/api';
export let book:Book;
let year=book.first_published_year?String(book.first_published_year):'';
let revision=book.bibliography_revision;
let busy=false;let error='';let message='';
async function save(event:SubmitEvent){
 event.preventDefault();busy=true;error='';message='';
 try{
  const value=year.trim()?Number(year):null;
  if(value!==null&&(!Number.isInteger(value)||value<1||value>new Date().getFullYear()+1))throw new Error('Enter a valid year, or leave it blank.');
  const result=await api<{revision:number}>(`/api/books/${book.id}/bibliography`,{first_published_year:value,revision},'PATCH');
  revision=result.revision;message='First publication year saved.';window.location.reload();
 }catch(e){error=(e as Error).message;}finally{busy=false;}
}
</script>
<form class="first-publication" onsubmit={save}>
<label class="library-field">First published (year)<input type="text" inputmode="numeric" pattern={'[0-9]{1,4}'} bind:value={year} disabled={busy} placeholder="Not recorded"/></label>
<p>The earliest publication year Open Library records for this work, which may be earlier than your edition’s date. Check it before relying on it. Leave blank if unknown. Saving a correction removes the imported source link.</p>
<button class="library-button secondary" disabled={busy}>{busy?'Saving…':'Save first publication year'}</button>
{#if error}<p role="alert" class="library-error">{error}</p>{/if}<p role="status">{message}</p>
</form>
<style>.first-publication{display:grid;gap:.75rem;max-width:36rem;margin-top:1.5rem;padding-top:1rem;border-top:1px solid var(--app-line)}.first-publication p{font-size:.875rem;line-height:1.5;color:var(--app-muted)}.first-publication .library-button{justify-self:start}.first-publication p:empty{display:none}</style>
