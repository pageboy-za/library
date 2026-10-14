<script lang="ts">
import {onMount} from 'svelte';
import {api} from '../../lib/api';
export let bookId:number;
type Discovery={book_id:number;description:string;subjects:string[];tags:string[];notes:string;revision:number};
type Published<T>=T|{saved:T;page_warning?:string};
type Enriched={discovery:Discovery;source_url:string;work_level:boolean;changed:boolean};
let data:Discovery={book_id:bookId,description:'',subjects:[],tags:[],notes:'',revision:0};
let subjects='';let tags='';let ready=false;let busy=false;let error='';let message='';let savedForm='';let publishedFields='';let sourceURL='';let workLevel=false;
function formKey(d:Discovery,ss:string,tt:string){return JSON.stringify([d.description,ss,tt,d.notes]);}
function staticKey(d:Discovery){return JSON.stringify([d.description,d.subjects]);}
function receive(d:Discovery){data=d;subjects=d.subjects.join(', ');tags=d.tags.join(', ');savedForm=formKey(data,subjects,tags);}
function normalize<T>(response:Published<T>):{value:T;warning:string}{if(typeof response==='object'&&response!==null&&'saved' in response){const wrapped=response as {saved:T;page_warning?:string};return {value:wrapped.saved,warning:wrapped.page_warning||''};}return {value:response as T,warning:(response as {page_warning?:string}).page_warning||''};}
$: dirty=ready&&formKey(data,subjects,tags)!==savedForm;
onMount(()=>{api<Discovery>(`/api/books/${bookId}/discovery`).then(v=>{receive(v);publishedFields=staticKey(v);ready=true;}).catch(e=>error=e.message);});
async function save(e:SubmitEvent){
 e.preventDefault();busy=true;error='';message='';
 try{
  const response=await api<Published<Discovery>>(`/api/books/${bookId}/discovery`,{...data,subjects:subjects.split(',').map(v=>v.trim()).filter(Boolean),tags:tags.split(',').map(v=>v.trim()).filter(Boolean)},'PATCH');
  const {value,warning}=normalize(response);receive(value);
  if(warning){message=warning;return;}
  message='Saved. These words are now searchable in Discover.';
  if(staticKey(value)!==publishedFields)window.location.reload();
 }catch(e){error=(e as Error).message;}finally{busy=false;}
}
async function enrich(){
 busy=true;error='';message='';
 try{
  const response=await api<Published<Enriched>>(`/api/books/${bookId}/discovery/enrich`,{});
  const {value,warning}=normalize(response);receive(value.discovery);sourceURL=value.source_url;workLevel=value.work_level;
  if(warning){message=warning;return;}
  message=value.changed?'Filled empty description and subjects. Review these details below.':'No missing description or subjects could be filled.';
  if(value.changed&&staticKey(value.discovery)!==publishedFields)window.location.reload();
 }catch(e){error=(e as Error).message;}finally{busy=false;}
}
</script>
<details class="discovery_editor"><summary>Subjects, tags and reading notes</summary>
<p class="library-muted">Keep your own themes and ideas here. Personal tags and notes stay in your local library.</p>
{#if ready}
<button type="button" class="library-button" onclick={enrich} disabled={busy||dirty}>{busy?'Working…':'Fill empty fields from Open Library'}</button>
<p class="library-muted">Fills only empty description and subjects; keeps your tags and notes. Save changes first before fetching. Subjects may describe the general work rather than this specific edition.</p>
{#if sourceURL}<p>Imported from <a href={sourceURL} target="_blank" rel="noreferrer">Open Library {workLevel?'work (general book information)':'edition'}</a>.</p>{/if}
<form onsubmit={save}>
<label class="library-field">Description<textarea bind:value={data.description} maxlength="20000" rows="4"></textarea></label>
<label class="library-field">Subjects (comma separated)<input bind:value={subjects} placeholder="e.g. History, Science"/></label>
<label class="library-field">Your tags (comma separated)<input bind:value={tags} placeholder="e.g. communication, books to revisit"/></label>
<label class="library-field">Your reading notes<textarea bind:value={data.notes} maxlength="20000" rows="4"></textarea></label>
<button class="library-button" disabled={busy}>{busy?'Saving…':'Save themes and notes'}</button>
</form>{/if}
{#if error}<p class="library-error" role="alert">{error}</p>{/if}<p role="status">{message}</p>
</details>
<style>@layer components{.discovery_editor{margin-block:var(--space-5);summary{cursor:pointer;font-weight:600}form{display:grid;gap:var(--space-3);margin-block:var(--space-3)}textarea{font:inherit;border:var(--border-width) solid var(--border);border-radius:var(--radius-main);padding:var(--space-3);background:var(--background);color:inherit;width:100%}}}</style>
