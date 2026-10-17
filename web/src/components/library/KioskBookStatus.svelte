<script lang="ts">
import {onMount} from 'svelte';
import {api,type Book} from '../../lib/api';
import OnLoanRibbon from './OnLoanRibbon.svelte';
export let bookId:number;
let text='';
let out=false;
onMount(()=>{
 let mounted=true;let sequence=0;
 const load=async()=>{const current=++sequence;try{const book=await api<Book>(`/api/books/${bookId}`);if(!mounted||current!==sequence)return;out=book.available<book.count;text=book.count===0?'No copies':book.available===0?'On loan':out?`${book.available} of ${book.count} available`:'Available';}catch{if(mounted&&current===sequence)text='Status unavailable';}};
 const refresh=(event:Event)=>{if(event instanceof CustomEvent&&event.detail?.bookId!==undefined&&event.detail.bookId!==bookId)return;void load();};
 void load();window.addEventListener('library-inventory-changed',refresh);window.addEventListener('focus',refresh);
 return()=>{mounted=false;sequence++;window.removeEventListener('library-inventory-changed',refresh);window.removeEventListener('focus',refresh);};
});
</script>
{#if text==='On loan'}
  <OnLoanRibbon/>
{:else if text&&text!=='Available'}
  <span class:out class="book-status">{text}</span>
{/if}
<style>
.book-status{position:absolute;left:8px;bottom:8px;right:8px;z-index:2;background:#fbf6eb;border:1px solid #cfb88e;border-radius:3px;padding:7px 9px;font:600 16px/1.25 'Library Inter',system-ui,sans-serif;color:#714514;box-shadow:0 2px 5px #1e261a16;text-align:center}
</style>
