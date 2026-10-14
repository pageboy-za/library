<script lang="ts">
import {onMount} from 'svelte';
import {api} from '../../lib/api';
let total:number|null=null;
onMount(()=>{let mounted=true;let sequence=0;const load=async()=>{const current=++sequence;try{const result=await api<{total:number}>('/api/loans?page_size=1');if(mounted&&current===sequence)total=result.total;}catch{if(mounted&&current===sequence)total=null;}};void load();window.addEventListener('library-inventory-changed',load);window.addEventListener('focus',load);return()=>{mounted=false;sequence++;window.removeEventListener('library-inventory-changed',load);window.removeEventListener('focus',load);};});
</script>
<a href="/loans/"><strong>Loans &amp; returns</strong>{#if total!==null}<span>{total===0?'No books out':`${total} ${total===1?'book':'books'} out`}</span>{/if}</a>
<style>a{display:grid;align-content:center;gap:4px;min-height:64px;padding:8px 18px;background:none;color:var(--app-ink);border:0;border-radius:4px;text-decoration:none;white-space:nowrap}strong{font-size:17px;font-weight:600}span{font-size:17px;color:#4b554d}a:hover{background:var(--app-soft)}a:focus-visible{outline:3px solid var(--app-accent);outline-offset:4px}@media(max-width:700px){a{min-height:56px;display:grid;gap:3px;padding-inline:10px}strong{font-size:16px}span{font-size:14px}}</style>
