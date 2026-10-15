<script lang="ts">
import {onDestroy} from 'svelte';
export let isbn:string;
let busy=false;let copied=false;let feedback='';let failed=false;let value:HTMLSpanElement;let reset:ReturnType<typeof setTimeout>;
onDestroy(()=>clearTimeout(reset));
async function copy(){if(busy)return;clearTimeout(reset);busy=true;feedback='';failed=false;try{if(!navigator.clipboard?.writeText)throw new Error('Clipboard unavailable');await navigator.clipboard.writeText(isbn);copied=true;feedback='ISBN copied.';reset=setTimeout(()=>{copied=false;feedback='';},2000);}catch{copied=false;const range=document.createRange();range.selectNodeContents(value);const selection=window.getSelection();selection?.removeAllRanges();selection?.addRange(range);failed=true;feedback='Could not copy automatically. Copy the selected ISBN manually.';}finally{busy=false;}}
</script>
<span class="isbn-copy"><span bind:this={value}>{isbn}</span><button type="button" aria-label="Copy ISBN" aria-busy={busy} onclick={copy}>{copied?'Copied':'Copy'}</button><span class="isbn-copy_feedback" class:is-error={failed} role="status" aria-live="polite">{feedback}</span></span>
<style>
@layer components {
  .isbn-copy{display:inline-flex;align-items:center;flex-wrap:wrap;gap:0 .625rem;max-width:100%;}
  .isbn-copy button{min-width:2.75rem;min-height:2.75rem;padding:.375rem .625rem;border:1px solid var(--app-line,var(--border));border-radius:.25rem;background:var(--app-surface,var(--background-3));color:var(--app-accent,var(--text));font-size:.8125rem;cursor:pointer;}
  .isbn-copy button:focus-visible{outline:2px solid var(--app-accent,var(--text));outline-offset:3px;}
  .isbn-copy button[aria-busy="true"]{cursor:wait;}
  .isbn-copy_feedback{flex-basis:100%;font-size:.8125rem;color:var(--app-muted,var(--text-2));}
  .isbn-copy_feedback:not(.is-error){position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap;}
  .isbn-copy_feedback:empty{display:none;}
}
</style>