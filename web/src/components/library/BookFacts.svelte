<script lang="ts">
import type {Book} from '../../lib/api';
import IsbnCopy from './IsbnCopy.svelte';
export let book:Pick<Book,'publisher'|'publication_date'|'isbn'>;
</script>

<dl class="book-facts" aria-label="Book details">
  <div><dt>Publisher</dt><dd>{book.publisher||'Not recorded'}</dd></div>
  <div><dt>Published</dt><dd>{book.publication_date||'Not recorded'}</dd></div>
  <div class="isbn"><dt>ISBN</dt><dd>{#if book.isbn}<IsbnCopy isbn={book.isbn}/>{:else}No ISBN{/if}</dd></div>
</dl>

<style>
.book-facts{display:grid;gap:.375rem;max-width:36rem;margin:.625rem 0 0;font-size:.9375rem;line-height:1.45;}
.book-facts>div{display:grid;grid-template-columns:5.5rem minmax(0,1fr);align-content:start;gap:1rem;}
dt{color:var(--app-muted,var(--text-2));}
dd{margin:0;overflow-wrap:anywhere;font-variant-numeric:tabular-nums;}
.isbn{align-items:center;}
@media(width<40rem){.book-facts>div{grid-template-columns:1fr;gap:.125rem;}}
</style>
