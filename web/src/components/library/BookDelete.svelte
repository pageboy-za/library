<script lang="ts">
import {tick} from 'svelte';
import {api,type Book} from '../../lib/api';
export let book:Book;export let disabled=false;export let busyChanged:(value:boolean)=>void=()=>{};
let confirming=false;let busy=false;let error='';let key='';let cancel:HTMLButtonElement;
async function open(){key=crypto.randomUUID();confirming=true;error='';await tick();cancel?.focus();}
async function remove(){if(busy||disabled)return;busy=true;busyChanged(true);error='';try{await api(`/api/books/${book.id}/delete`,{revision:book.revision,expected_copy_count:book.total_copy_count,request_key:key});window.location.href='/staff/settings/?deleted=1';}catch(e){error=(e as Error).message;}finally{busy=false;busyChanged(false);}}
</script>
<section class="delete-book" aria-label="Delete book">
{#if !confirming}<button class="library-button secondary" disabled={disabled||book.copies.some(copy=>copy.out)} onclick={open}>Delete book</button>{#if book.copies.some(copy=>copy.out)}<p>Return all copies on loan before deleting this book.</p>{/if}
{:else}<div role="group" aria-label="Confirm book deletion"><h3>Delete “{book.title}”?</h3><p>This hides the book from the active catalogue and archives its {book.count} active {book.count===1?'copy':'copies'}. It retains {book.total_copy_count} {book.total_copy_count===1?'copy':'copies'} in total, including any previously archived copies, with their shelf records, covers and loan history. You can restore it in Settings → Deleted books.</p><div class="library-actions"><button bind:this={cancel} class="library-button secondary" disabled={busy} onclick={()=>confirming=false}>Keep book</button><button class="library-button" disabled={busy||disabled} onclick={remove}>{busy?'Deleting…':'Confirm delete book'}</button></div></div>{/if}
{#if error}<p role="alert" class="library-error">{error}</p>{/if}
</section>
<style>@layer components{.delete-book{border-top:1px solid var(--app-line,var(--border));padding-top:1rem;margin-top:1rem;display:grid;gap:.75rem;font-size:.875rem}.delete-book p{max-width:50rem;line-height:1.6}.delete-book h3{font-size:1rem;margin-bottom:.5rem}}</style>
