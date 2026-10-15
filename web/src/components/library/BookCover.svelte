<script lang="ts">
export let title:string;
export let coverUrl='';
export let thumbnailUrl='';
export let size:'small'|'medium'|'large'='small';
let failedThumbnail='';
let failedOriginal='';
function localCover(url:string){return /^\/(?:covers\/|api\/books\/)/.test(url)?url:'';}
$: original=localCover(coverUrl);
$: thumbnail=localCover(thumbnailUrl);
$: source=thumbnail&&failedThumbnail!==thumbnail?thumbnail:original&&failedOriginal!==original?original:'';
function failed(){if(source===thumbnail)failedThumbnail=source;if(source===original)failedOriginal=source;}
</script>

<span class="book-cover" class:medium={size==='medium'} class:large={size==='large'}>
  {#if source}
    <img src={source} alt={`Cover of ${title}`} loading="lazy" decoding="async" onerror={failed}/>
  {:else}
    <span class="book-cover_missing" role="img" aria-label={`Cover unavailable for ${title}`}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 3h14v18H5zM8 3v18M11 8h5M11 11h4"/></svg>
      <span>{title}</span><small>Cover unavailable</small>
    </span>
  {/if}
</span>

<style>
@layer components {
  .book-cover{display:block;flex:0 0 auto;width:3.5rem;height:5.25rem;min-width:0;overflow:hidden;border-radius:.15rem}
  .book-cover.medium{width:6rem;height:9rem}.book-cover.large{width:12rem;height:18rem;max-width:100%}
  .book-cover img{display:block;width:100%;height:100%;object-fit:contain;object-position:center}
  .book-cover_missing{display:flex;flex-direction:column;justify-content:center;align-items:center;gap:.3rem;width:100%;height:100%;padding:.3rem;background:var(--app-placeholder,var(--background-3));border:1px solid var(--app-line,var(--border));color:var(--app-ink,var(--text-1));text-align:center;font-size:.6rem;line-height:1.2;overflow:hidden}
  .book-cover_missing svg{width:1.2rem;height:1.2rem;flex-shrink:0;fill:none;stroke:currentColor;stroke-width:1.3}
  .book-cover_missing>span{display:-webkit-box;-webkit-line-clamp:3;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
  .book-cover_missing small{font-size:.55rem}.medium .book-cover_missing{font-size:.85rem;padding:.5rem}.large .book-cover_missing{font-size:1.2rem;padding:1rem}
}
</style>
