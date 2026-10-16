<script lang="ts">
import {onMount,onDestroy} from 'svelte';
import {api,type Book} from '../../lib/api';
import CoverPicker from './CoverPicker.svelte';
import DiscoveryEditor from './DiscoveryEditor.svelte';
import BookWorkspace from './BookWorkspace.svelte';
import QuickScan from './QuickScan.svelte';
import BookFacts from './BookFacts.svelte';
import {openLibraryUrl} from '../../lib/openLibrary';
export let bookId:number|undefined=undefined;
let book:Book|null=null;let error='';let description='';let subjects:string[]=[];let editing=false;let stopped=false;
$: openLibraryHref=book ? openLibraryUrl(book) : '';
let coverTimer:ReturnType<typeof setTimeout>;let attempts=0;
async function load(){
 error='';
 const id=bookId??Number(window.location.pathname.split('/').filter(Boolean).at(-1));
 try{
  const value=await api<Book>(`/api/books/${id}`);
  if(stopped)return;
  book=value;
  api<{description:string;subjects:string[]}>(`/api/books/${id}/discovery`).then(v=>{if(!stopped){description=v.description;subjects=v.subjects;}}).catch(()=>{});
  if(!value.cover_url&&value.isbn&&attempts++<10)coverTimer=setTimeout(load,3000);
 }catch(e){if(!stopped)error=(e as Error).message;}
}
function changeCover(){const picker=document.querySelector<HTMLDetailsElement>('.book-page .cover-picker');if(picker){picker.open=true;picker.scrollIntoView({block:'start'});picker.querySelector<HTMLElement>('summary')?.focus();}}
onMount(()=>{load();const changed=(event:Event)=>{const detail=(event as CustomEvent<{bookId:number;open:boolean}>).detail;if(detail.bookId===book?.id)editing=detail.open;};window.addEventListener('library-book-editor-changed',changed);return()=>window.removeEventListener('library-book-editor-changed',changed);});
onDestroy(()=>{stopped=true;clearTimeout(coverTimer);});
</script>
<svelte:head><title>{book ? `${book.title} · Home Library` : 'Book · Home Library'}</title></svelte:head>
{#if error}<p role="alert" class="library-error">{error}</p><button class="library-button" onclick={load}>Try again</button>{/if}
{#if book}
<article class="book-page" data-book-id={book.id} data-book-revision={book.revision}>
  <div class="book-page_top"><a class="book-page_back" href="/staff/">← Back to library</a><QuickScan/></div>
  <div class="book-page_body">
    <aside class="book-page_cover" aria-label="Book cover">
      {#if book.cover_url}<img src={book.cover_url} alt={`Cover of ${book.title}`}/>{:else}<div class="book-page_no-cover"><span>Cover unavailable</span></div>{/if}
      <button type="button" class="book-page_cover-action" onclick={changeCover}>Change cover</button>
    </aside>
    <div class="book-page_work">
    <header class="book-page_metadata">
      
      <h1>{book.title}</h1>
      {#if book.subtitle}<p class="book-page_subtitle">{book.subtitle}</p>{/if}
      <p class="book-page_authors">{book.authors.join(', ')||'Author not recorded'}</p>
      <BookFacts {book}/>
      <div class="book-page_actions"><button type="button" onclick={()=>window.dispatchEvent(new CustomEvent('library-edit-book-details',{detail:book!.id}))} aria-expanded={editing}>{editing?'Close editor':'Edit details'}</button>{#if openLibraryHref}<a href={openLibraryHref} target="_blank" rel="noreferrer">View on Open Library ↗</a>{/if}</div>
    </header>
    <BookWorkspace bookId={book.id}/>
    </div>
  </div>
  <div class="book-page_secondary">
    <CoverPicker bookId={book.id}/>
    <DiscoveryEditor bookId={book.id}/>
    {#if description}<section class="book-page_about"><h2>About this book</h2><p>{description}</p></section>{/if}
    {#if subjects.length}<section class="book-page_subjects"><h2>Subjects</h2><ul>{#each subjects as subject}<li><a href={`/staff/catalogue/?subject=${encodeURIComponent(subject)}`}>{subject}</a></li>{/each}</ul></section>{/if}
  </div>
</article>
{:else if !error}<p role="status">Loading book…</p>{/if}
<style>
@layer components {
  .book-page_subtitle {font-size:1.125rem;line-height:1.5;color:var(--app-muted);overflow-wrap:anywhere;}
  .book-page {display:grid;gap:1.5rem;color:var(--app-ink,var(--text));min-width:0;}
  .book-page_top {display:flex;align-items:center;justify-content:space-between;gap:1rem;padding-bottom:.125rem;}
  .book-page_back {display:inline-flex;align-items:center;min-height:2.75rem;color:var(--app-muted,var(--text-2));font-size:.875rem;text-decoration:none;}
  .book-page_back:hover {color:var(--app-accent,var(--text));text-decoration:underline;}
  .book-page_body {display:grid;grid-template-columns:17.5rem minmax(0,1fr);gap:2.5rem;align-items:start;}
  .book-page_cover {display:grid;gap:.75rem;align-content:start;img{display:block;width:100%;height:auto;max-height:28rem;object-fit:contain;object-position:left top;filter:drop-shadow(0 .25rem .375rem #201a151a);}}
  .book-page_cover-action {justify-self:start;min-height:2.75rem;background:transparent;border:0;padding:.5rem 0;font-size:.875rem;color:var(--app-muted,var(--text-2));text-decoration:underline;text-underline-offset:.2em;}
  .book-page_no-cover {min-height:23rem;display:grid;place-items:center;padding:1.5rem;text-align:center;border:1px solid var(--app-line,var(--border));background:var(--app-surface,var(--background-3));color:var(--app-muted,var(--text-2));font-size:.875rem;}
  .book-page_work {display:grid;gap:1.25rem;min-width:0;}
  .book-page_metadata {display:grid;gap:.5rem;min-width:0;}
  .book-page h1 {font-family:var(--app-serif,Georgia,serif);font-weight:400;font-size:clamp(2rem,3.4vw,2.75rem);line-height:1.08;letter-spacing:-.025em;max-width:36ch;overflow-wrap:anywhere;}
  .book-page_authors {font-size:1.0625rem;color:var(--app-muted,var(--text-2));}
  .book-page_actions {display:flex;flex-wrap:wrap;gap:.25rem 1.25rem;button,a{display:inline-flex;align-items:center;min-height:2.75rem;padding:.5rem 0;background:transparent;border:0;font-size:.875rem;color:var(--app-muted,var(--text-2));text-decoration:underline;text-underline-offset:.2em;}}
  .book-page_secondary {display:grid;gap:1rem;padding-top:1.25rem;border-top:1px solid var(--app-line,var(--border));min-width:0;}
  .book-page_secondary :global(details) {max-width:100%;font-size:.875rem;}
  .book-page_secondary :global(summary) {min-height:2.75rem;display:flex;align-items:center;}
  .book-page_about,.book-page_subjects {display:grid;gap:.75rem;max-width:75ch;h2{font-family:var(--app-serif,Georgia,serif);font-weight:400;font-size:1.375rem;line-height:1.25;}}
  .book-page_about p {font-size:.9375rem;line-height:1.65;white-space:pre-line;overflow-wrap:anywhere;}
  .book-page_subjects ul {display:flex;flex-wrap:wrap;gap:.5rem 1rem;list-style:none;padding:0;li{font-size:.875rem;color:var(--app-muted,var(--text-2));overflow-wrap:anywhere;}}
  @media(width<70rem) {.book-page_body{grid-template-columns:14rem minmax(0,1fr);gap:1.75rem;}.book-page_no-cover{min-height:18rem;}}
  @media(width<58rem) {.book-page_body{grid-template-columns:8rem minmax(0,1fr);gap:1.25rem;}.book-page_work{display:contents;}.book-page_body :global(.workspace){grid-column:1/-1;}.book-page_cover{grid-row:1;}.book-page_metadata{grid-column:2;grid-row:1;}.book-page_no-cover{min-height:11rem;padding:.75rem;}.book-page_cover-action{font-size:.8125rem;}.book-page h1{font-size:2rem;}}
  @media(width<40rem) {.book-page_top{align-items:stretch;flex-direction:column;gap:.5rem;}.book-page_body{grid-template-columns:6.5rem minmax(0,1fr);gap:1rem;}.book-page_no-cover{min-height:9rem;}.book-page h1{font-size:1.75rem;}.book-page_authors{font-size:.9375rem;}}
}
</style>
