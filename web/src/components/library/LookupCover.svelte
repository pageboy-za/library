<script lang="ts">
export let isbn:string;
export let title:string;
let loading=true;
let failed=false;
let requested='';
$: code=isbn.replace(/[\s-]/g,'').toUpperCase();
$: url=/^(?:\d{9}[\dX]|\d{13})$/.test(code)
  ? `https://covers.openlibrary.org/b/isbn/${code}-L.jpg?default=false` : '';
$: if(url!==requested){requested=url;loading=true;failed=false;}
</script>

<figure>
  <div class="cover" aria-busy={loading&&!!url&&!failed}>
    {#if url&&!failed}
      {#key url}
        <img src={url} alt={`Cover of ${title||'the scanned book'}`} referrerpolicy="no-referrer"
          onload={()=>loading=false} onerror={()=>{failed=true;loading=false;}}/>
      {/key}
      {#if loading}<span class="placeholder" role="status">Loading cover…</span>{/if}
    {:else}
      <span class="placeholder">Cover unavailable</span>
    {/if}
  </div>
  <figcaption>{failed||!url?'You can add a cover after saving.':'Cover from Open Library. Check it matches your edition.'}</figcaption>
</figure>

<style>
figure{margin:0;display:grid;gap:12px}
.cover{position:relative;min-height:220px;display:grid;place-items:center;background:var(--app-paper);border:1px solid var(--app-line)}
img{display:block;width:100%;height:auto;max-height:340px;object-fit:contain}
.placeholder{padding:24px;text-align:center;color:var(--app-muted);font-size:16px}
img+.placeholder{position:absolute;inset:0;display:grid;place-items:center;background:var(--app-paper)}
figcaption{font-size:14px;line-height:1.5;color:var(--app-muted)}
@media(width<48rem){.cover{min-height:160px}img{max-height:230px}}
</style>
