<script lang="ts">
import {onMount} from 'svelte';import {api,type Book} from '../../lib/api';
let books:Book[]=[];let loading=true;let busy=false;let error='';let message='';let warning='';
const keys=new Map<number,string>();
async function load(){try{books=await api<Book[]>('/api/deleted-books');}catch(e){error=(e as Error).message;}finally{loading=false;}}
onMount(()=>{if(new URLSearchParams(window.location.search).has('deleted'))message='Book deleted. You can restore it below.';load();});
async function restore(book:Book){if(busy)return;busy=true;error='';warning='';if(!keys.has(book.id))keys.set(book.id,crypto.randomUUID());try{const result=await api<{page_warning?:string}>(`/api/books/${book.id}/restore`,{revision:book.revision,expected_copy_count:book.total_copy_count,request_key:keys.get(book.id)});keys.delete(book.id);message='Book restored.';warning=result.page_warning||'';await load();}catch(e){error=(e as Error).message;}finally{busy=false;}}
</script>
<section aria-label="Deleted books" class="deleted-books"><h2>Deleted books</h2><p>Restore a deleted book with its previous shelves, copies and loan history. Copies archived before deletion stay archived.</p>
{#if error}<p role="alert" class="library-error">{error}</p>{/if}<p role="status">{message}</p>{#if warning}<p role="status">{warning}</p>{/if}
{#if loading}<p>Loading deleted books…</p>{:else if !books.length}<p>No deleted books.</p>{:else}<ul>{#each books as book}<li><div><strong>{book.title}</strong><p>{book.total_copy_count} {book.total_copy_count===1?'copy':'copies'} retained{book.isbn?` · ISBN ${book.isbn}`:''}</p></div><button class="library-button secondary" disabled={busy} onclick={()=>restore(book)}>Restore book<span class="sr-only">: {book.title}</span></button></li>{/each}</ul>{/if}</section>
<style>@layer components{.deleted-books h2{font-family:Georgia,serif;font-size:var(--h4)}.deleted-books{display:grid;gap:1rem;padding-block:1.5rem;border-top:1px solid var(--border)}.deleted-books ul{display:grid;gap:1rem}.deleted-books li{display:flex;align-items:center;justify-content:space-between;gap:1rem;flex-wrap:wrap}.deleted-books p{line-height:1.5}.deleted-books p:empty{display:none}.sr-only{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}}</style>
