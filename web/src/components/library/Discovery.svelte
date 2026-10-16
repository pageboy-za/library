<script lang="ts">
import BookCover from './BookCover.svelte';
import {onMount} from 'svelte';
import {api,type Book} from '../../lib/api';
type Result={book:Book;reasons:string[];score:number};let query='';let results:Result[]=[];let error='';let busy=false;let searched=false;let sequence=0;let searchInput:HTMLInputElement;onMount(()=>searchInput?.focus());
async function search(e:SubmitEvent){e.preventDefault();const current=++sequence;busy=true;error='';try{const found=await api<Result[]>('/api/discovery?q='+encodeURIComponent(query));if(current===sequence){results=found;searched=true;}}catch(e){if(current===sequence)error=(e as Error).message;}finally{if(current===sequence)busy=false;}}
</script>
<form onsubmit={search} class="discovery_search"><label class="library-field">What would you like to explore?<input bind:this={searchInput} bind:value={query} required maxlength="500" placeholder="e.g. communication, gardening, history"/></label><button class="library-button" disabled={busy}>{busy?'Searching…':'Find related books'}</button></form>
<p class="library-muted">Matches words in titles, authors, subjects, descriptions and your own tags and notes. These are keyword matches, not semantic recommendations.</p>
{#if error}<p role="alert" class="library-error">{error}</p>{/if}
<div aria-live="polite">{#if searched&&!busy}<p>{results.length?`${results.length} matching books (up to 50 shown).`:'No matching books yet. Add subjects or personal tags to your books to improve discovery.'}</p>{/if}</div>
<div class="discovery_results">{#each results as result}<article class="discovery_card"><BookCover title={result.book.title} coverUrl={result.book.cover_url} thumbnailUrl={result.book.cover_thumbnail_url} size="medium"/><div><h2><a href={'/staff/books/'+result.book.id+'/'}>{result.book.title}</a></h2><p>{result.book.authors.join(', ')}</p><p>{result.book.available} of {result.book.count} copies available</p><ul>{#each result.reasons as reason}<li>{reason}</li>{/each}</ul></div></article>{/each}</div>
<style>@layer components{.discovery_search{display:flex;align-items:end;gap:var(--space-3);label{flex:1}@media(width<48rem){flex-direction:column;align-items:stretch}}.discovery_results{display:grid;gap:var(--space-4)}.discovery_card{display:flex;gap:var(--space-4);padding:var(--space-4);border:var(--border-width) solid var(--border);border-radius:var(--radius-main);>div{min-width:0;overflow-wrap:anywhere}h2{font-size:1.5rem}ul{padding-inline-start:1.2rem}}}</style>
