<script lang="ts">
import {onMount} from 'svelte';
import {api, type Book, type Copy} from '../../lib/api';

export let initialBookId:number;
export let variant:'default'|'kiosk'='default';

type Borrower = {id:number; name:string; notes:string};
type Loan = {id:number; copy_id:number; borrower_id:number; borrower:string; checked_out_at:string; due_date:string|null; returned_at:string|null};
type Pending = {
  kind:'lend'|'return'; bookId:number; title:string; copyId:number; copyLabel:string;
  borrowerId:number|null; borrowerName:string; borrowerKey:string; requestKey:string;
  loanId:number|null;
};
class MutationError extends Error {
  rejected:boolean;
  constructor(message:string, rejected=false) { super(message); this.rejected=rejected; }
}

let bookId=initialBookId;
let book:Book|null=null;
let borrowers:Borrower[]=[];
let loans:Loan[]=[];
let selectedCopyId:number|null=null;
let borrowerName='';
let newPerson=false;
let selectedBorrowerId:number|null=null;
let loading=true;
let refreshing=false;
let busy=false;
let error='';
let loadError='';
let message='';
let navigationNotice='';
let pending:Pending|null=null;
let sequence=0;
let mounted=false;
const normalize=(value:string)=>value.trim().replace(/\s+/g,' ').toLocaleLowerCase();

$: copy=book?.copies.find(item=>item.id===selectedCopyId) ?? null;
$: loan=copy ? loans.find(item=>item.copy_id===copy.id&&!item.returned_at) : undefined;
$: query=normalize(borrowerName);
$: exactMatches=borrowers.filter(person=>normalize(person.name)===query);
$: suggestions=query&&!selectedBorrowerId
  ? exactMatches.length>1 ? exactMatches : borrowers.filter(person=>normalize(person.name).includes(query)).slice(0,6)
  : [];
$: locked=busy||refreshing||!!pending;
$: canLend=!!query&&(selectedBorrowerId!==null||exactMatches.length<2);
$: shortcutBorrowers=borrowers.filter(person=>borrowers.filter(other=>normalize(other.name)===normalize(person.name)).length===1).slice(0,3);

function loanStart(value:string) { const date=new Date(value); if(Number.isNaN(date.valueOf()))return ''; return date.toDateString()===new Date().toDateString()?'Borrowed today':`Since ${date.toLocaleDateString('en-GB',{day:'numeric',month:'short',year:'numeric'})}`; }

function resetForm() { borrowerName=''; selectedBorrowerId=null; newPerson=false; }
function copyName(value:Copy) { return value.label||`Copy ${value.id}`; }
function shelfName(value:string) {
  const parts=value.split(' / ');
  if(parts.length===2) {
    const place=normalize(parts[0]).replace(/\s+(?:bookshelf|bookcase|shelf|cabinet)$/,'');
    if(place===normalize(parts[1]))return parts[0];
  }
  return value||'Shelf not assigned';
}

function publishStatus() {
  if(!book)return;
  const selected=book.copies.find(item=>item.id===selectedCopyId);
  const selectedLoan=loans.find(item=>item.copy_id===selected?.id&&!item.returned_at);
  window.dispatchEvent(new CustomEvent('library-kiosk-status',{
    detail:{
      bookId:book.id,out:!!(selected?.out||selectedLoan),
      shelf:selected?shelfName(selected.shelf):'',available:book.available,loanCount:loans.length
    }
  }));
}

function chooseCopy(event:Event) {
  selectedCopyId=Number((event.currentTarget as HTMLSelectElement).value);
  resetForm();
  error='';
  message='';
  publishStatus();
}

async function load() {
  const request=++sequence;
  const target=bookId;
  refreshing=true;
  loadError='';
  try {
    const [nextBook,nextBorrowers,nextLoans]=await Promise.all([
      api<Book>(`/api/books/${target}`),
      api<Borrower[]>('/api/borrowers'),
      api<Loan[]>(`/api/books/${target}/loans`)
    ]);
    if(!mounted||request!==sequence||target!==bookId)return;
    book=nextBook;
    borrowers=nextBorrowers;
    loans=nextLoans.filter(item=>!item.returned_at);
    if(!book.copies.some(item=>item.id===selectedCopyId))
      selectedCopyId=(book.copies.find(item=>!item.out)??book.copies[0])?.id??null;
    publishStatus();
  } catch(cause) {
    if(mounted&&request===sequence)loadError=(cause as Error).message||'Could not load lending details.';
  } finally {
    if(mounted&&request===sequence) { loading=false; refreshing=false; }
  }
}

function select(event:Event) {
  const incoming=event as CustomEvent<{bookId:number}>;
  const next=incoming.detail?.bookId;
  if(!Number.isInteger(next)||next<=0)return;
  if((busy||pending)&&next!==bookId) {
    incoming.preventDefault();
    navigationNotice='Finish this lending update before choosing another book.';
    window.dispatchEvent(new CustomEvent('library-kiosk-selection-blocked',{
      detail:{bookId,requestedBookId:next}
    }));
    return;
  }
  if(next!==bookId) {
    bookId=next;
    book=null;
    loans=[];
    selectedCopyId=null;
    resetForm();
    error='';
    message='';
    navigationNotice='';
    loading=true;
  }
  if(!busy)void load();
}

function guardNavigation(event:Event) {
  if(!busy&&!pending)return;
  event.preventDefault();
  navigationNotice='Keep this book open until the lending update is confirmed.';
}

onMount(()=>{
  mounted=true;
  const refresh=(event:Event)=>{
    if(event instanceof CustomEvent&&event.detail?.source==='kiosk-lending')return;
    if(!busy)void load();
  };
  const pageshow=(event:PageTransitionEvent)=>{if(event.persisted&&!busy)void load();};
  window.addEventListener('library-kiosk-select',select);
  window.addEventListener('library-kiosk-navigation',guardNavigation);
  window.addEventListener('library-inventory-changed',refresh);
  window.addEventListener('focus',refresh);
  window.addEventListener('pageshow',pageshow);
  void load();
  window.dispatchEvent(new CustomEvent('library-kiosk-ready'));
  return()=>{
    mounted=false;
    sequence++;
    window.removeEventListener('library-kiosk-select',select);
    window.removeEventListener('library-kiosk-navigation',guardNavigation);
    window.removeEventListener('library-inventory-changed',refresh);
    window.removeEventListener('focus',refresh);
    window.removeEventListener('pageshow',pageshow);
  };
});

function chooseBorrower(person:Borrower) {
  if(locked)return;
  selectedBorrowerId=person.id;
  borrowerName=person.name;
  error='';
}

// Keep the HTTP status so a rejected request can be distinguished from a lost response.
async function post(url:string, body:Record<string,unknown>) {
  const response=await fetch(url,{
    method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)
  });
  const result=await response.json().catch(()=>null);
  if(!response.ok)throw new MutationError(
    result?.error||'Could not complete the request.',response.status>=400&&response.status<500
  );
  if(!Number.isInteger(result?.id)||result.id<=0)
    throw new MutationError('The server response could not be confirmed.');
  return result.id as number;
}

async function perform(operation:Pending) {
  if(busy)return;
  busy=true;
  error='';
  message='';
  navigationNotice='';
  try {
    if(operation.kind==='lend') {
      if(operation.borrowerId===null) {
        operation.borrowerId=await post('/api/borrowers',{
          name:operation.borrowerName,notes:'',request_key:operation.borrowerKey
        });
        selectedBorrowerId=operation.borrowerId;
      }
      await post('/api/loans',{
        copy_id:operation.copyId,borrower_id:operation.borrowerId,
        due_date:'',request_key:operation.requestKey
      });
    } else {
      await post(`/api/loans/${operation.loanId}/return`,{request_key:operation.requestKey});
    }
    pending=null;
    navigationNotice='';
    resetForm();
    message=operation.kind==='lend'
      ? `${operation.title} · ${operation.copyLabel} lent to ${operation.borrowerName}.`
      : `${operation.title} · ${operation.copyLabel} returned.`;
    window.dispatchEvent(new CustomEvent('library-inventory-changed',{
      detail:{source:'kiosk-lending',bookId:operation.bookId,copyId:operation.copyId}
    }));
    window.dispatchEvent(new CustomEvent('library-kiosk-complete',{
      detail:{bookId:operation.bookId,copyId:operation.copyId,action:operation.kind,message}
    }));
  } catch(cause) {
    if(cause instanceof MutationError&&cause.rejected) {
      pending=null;
      error=cause.message;
    } else {
      // The same operation and keys survive a retry, including a partially created borrower.
      error='The update could not be confirmed. Keep this book open and retry safely to finish checking it.';
    }
  } finally {
    if(mounted)await load();
    busy=false;
  }
}

function lend(event?:SubmitEvent) {
  event?.preventDefault();
  if(locked||loadError||!book||!copy||copy.out||loan)return;
  const name=borrowerName.trim();
  if(!name)return;
  let borrowerId=selectedBorrowerId;
  if(borrowerId!==null&&!borrowers.some(person=>person.id===borrowerId)) {
    error='That borrower is no longer available. Choose a borrower again.';
    selectedBorrowerId=null;
    return;
  }
  if(borrowerId===null) {
    if(exactMatches.length>1) {
      error='More than one borrower has that name. Choose the right person below.';
      return;
    }
    borrowerId=exactMatches[0]?.id??null;
  }
  pending={
    kind:'lend',bookId,title:book.title,copyId:copy.id,copyLabel:copyName(copy),
    borrowerId,borrowerName:borrowers.find(person=>person.id===borrowerId)?.name??name,
    borrowerKey:crypto.randomUUID(),requestKey:crypto.randomUUID(),loanId:null
  };
  void perform(pending);
}

function returnBook() {
  if(locked||loadError||!book||!copy||!loan)return;
  pending={
    kind:'return',bookId,title:book.title,copyId:copy.id,copyLabel:copyName(copy),
    borrowerId:loan.borrower_id,borrowerName:loan.borrower,
    borrowerKey:'',requestKey:crypto.randomUUID(),loanId:loan.id
  };
  void perform(pending);
}
</script>

<section class="kiosk-lending" class:compact={variant==='kiosk'} aria-label="Lend or return this book" aria-busy={loading||busy||refreshing}>
  {#if loading}
    <p class="quiet" role="status">Loading lending details…</p>
  {:else if book}
    {#if copy}
      <div class="copy-status">
        {#if variant==='kiosk'}
          {#if book.cover_url}<img class="selected-thumb" src={book.cover_url} alt=""/>{:else}<span class="selected-thumb no-thumb" aria-hidden="true"><span>{book.title}</span><small>{book.authors[0]}</small></span>{/if}
          <a class="current-book" href={`/books/${book.id}/`}><strong>{book.title}</strong><span>{book.authors.join(', ')}</span></a>
        {/if}
        {#if book.copies.length>1}
          <label class="field copy-choice">Copy
            <select value={selectedCopyId??''} disabled={locked} onchange={chooseCopy}>
              {#each book.copies as item (item.id)}
                <option value={item.id}>{copyName(item)} · {item.out?'On loan':'Available'}</option>
              {/each}
            </select>
          </label>
        {/if}
        <div class="availability">
          <strong class:out={copy.out||!!loan}>{copy.out||loan?'On loan':'Available to lend'}</strong>
          <span class="home-shelf">{#if variant==='kiosk'}<small>{copy.out||loan?'Home shelf':'Shelf'}</small>{/if}{shelfName(copy.shelf)}</span>
        </div>
      </div>
      <div class="circulation-actions">
        {#if pending}
          <p class="quiet pending-copy">{pending.kind==='lend'?'Lending':'Returning'} {pending.copyLabel.toLocaleLowerCase()}{pending.kind==='lend'?` to ${pending.borrowerName}`:''}.</p>
        {:else if loan}
          <div class="return-row">
            <div class="return-details">
              <p class="loan-person"><span>{variant==='kiosk'?'Borrowed by':'With'} <strong>{loan.borrower}</strong></span>{#if variant==='kiosk'&&loan.checked_out_at}<span class="quiet">{loanStart(loan.checked_out_at)}</span>{/if}{#if loan.due_date}<span class="quiet">Due {new Date(`${loan.due_date}T12:00:00`).toLocaleDateString('en-GB',{day:'numeric',month:'short',year:'numeric'})}</span>{/if}</p>
            </div>
            <button type="button" class="primary" disabled={locked||!!loadError} onclick={returnBook}>Return book</button>
          </div>
        {:else if copy.out}
          <p class="quiet">This copy is on loan. Refresh to load its return details.</p>
          <button type="button" class="text-action" disabled={busy||refreshing} onclick={()=>load()}>Refresh status</button>
        {:else}
          {#if variant==='kiosk'&&shortcutBorrowers.length&&!newPerson}
            <div class="quick-lend"><p class="task-heading">Lend this book to</p><div class="borrower-shortcuts">
              {#each shortcutBorrowers as person (person.id)}<button type="button" aria-label={`Lend to ${person.name}`} disabled={locked||!!loadError} onclick={()=>{chooseBorrower(person);lend();}}>{person.name}</button>{/each}
              <button type="button" class="new-person" disabled={locked||!!loadError} onclick={()=>{newPerson=true;}}>Someone else</button>
            </div></div>
          {:else}
          <form onsubmit={lend}>
            <div class="lend-row">
              <label class="field">{variant==='kiosk'?'Lend this book':'Lend to'}
                <input aria-label="Borrower’s name" value={borrowerName} placeholder="Borrower’s name" maxlength="200" autocomplete="off" required disabled={locked||!!loadError} oninput={(event)=>{borrowerName=event.currentTarget.value;selectedBorrowerId=null;error='';message='';}}/>
              </label>
              <button class="primary" disabled={locked||!!loadError||(variant!=='kiosk'&&!canLend)||(!!query&&exactMatches.length>1&&!selectedBorrowerId)}>Lend book</button>
            </div>
            {#if suggestions.length}
              <ul class="suggestions" aria-label="Existing borrowers">
                {#each suggestions as person (person.id)}
                  <li><button type="button" disabled={locked} onclick={()=>chooseBorrower(person)}><span>{person.name}</span>{#if borrowers.filter(item=>normalize(item.name)===normalize(person.name)).length>1}<small>Borrower {person.id}{person.notes?` · ${person.notes}`:''}</small>{/if}</button></li>
                {/each}
              </ul>
            {/if}
            {#if query&&!selectedBorrowerId&&exactMatches.length>1}<p class="field-note">Choose which {borrowerName.trim()} from the borrowers above.</p>{/if}
            {#if query&&!selectedBorrowerId&&!exactMatches.length}<p class="field-note">New borrower — saved when you lend.</p>{/if}
            {#if variant==='kiosk'&&shortcutBorrowers.length}<button type="button" class="text-action" disabled={locked} onclick={()=>resetForm()}>Choose a saved borrower</button>{/if}
          </form>
          {/if}
        {/if}
      </div>
    {:else}
      <p class="quiet">No copies recorded. Add a copy from the book’s full details to lend it.</p>
    {/if}
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if navigationNotice}<p class="quiet field-note" role="status">{navigationNotice}</p>{/if}
  {#if pending}<button type="button" class="primary" disabled={busy||refreshing} onclick={()=>pending&&perform(pending)}>{busy?'Saving…':pending.kind==='lend'?'Retry lending safely':'Retry return safely'}</button>{/if}
  {#if loadError}<div class="load-error"><p class="error" role="alert">{loadError}</p><button type="button" class="text-action" disabled={busy||refreshing} onclick={()=>load()}>Refresh lending details</button></div>{/if}
  {#if message}<p class="success" role="status">{message}</p>{/if}
</section>

<style>
.kiosk-lending{color:var(--kiosk-ink,#26382d);display:grid;gap:1rem;min-width:0;font-size:1rem;line-height:1.45;}
.kiosk-lending p,.kiosk-lending ul{margin:0;}
.copy-status,.circulation-actions{display:grid;gap:1rem;min-width:0;}
.lend-row,.return-row,.return-details{display:grid;gap:.75rem;min-width:0;}
.availability{display:grid;gap:.25rem;}
.availability strong{display:flex;align-items:center;gap:.5rem;font-size:1rem;font-weight:600;color:var(--kiosk-green,#315440);}
.availability strong::before{content:'';width:.4375rem;height:.4375rem;border-radius:50%;background:currentColor;flex:none;}
.availability strong.out{color:var(--kiosk-ink,#26382d);}
.home-shelf,.quiet{color:var(--kiosk-muted,#59645a);overflow-wrap:anywhere;}
.home-shelf{font-size:.9375rem;}
form,.field{display:grid;gap:.5rem;min-width:0;}
form{gap:.75rem;}
.field{font-size:.875rem;font-weight:500;}
input,select,button{box-sizing:border-box;max-width:100%;min-height:3rem;border-radius:.1875rem;font:inherit;}
input,select{width:100%;border:1px solid var(--kiosk-line,#d9ded2);background:var(--kiosk-paper,#fafbf7);color:var(--kiosk-ink,#26382d);padding:.625rem .75rem;font-size:1rem;font-weight:400;}
button{cursor:pointer;}
button:disabled{cursor:default;}
input:focus-visible,select:focus-visible,button:focus-visible{outline:2px solid var(--kiosk-green,#315440);outline-offset:3px;}
.primary{width:100%;padding:.75rem 1rem;border:1px solid var(--kiosk-green,#315440);background:var(--kiosk-green,#315440);color:#fff;font-size:1rem;font-weight:600;text-align:center;}
.primary:hover:not(:disabled){filter:brightness(.92);}
.primary:disabled{background:color-mix(in srgb,var(--kiosk-green,#315440) 10%,var(--kiosk-paper,#fafbf7));color:var(--kiosk-muted,#59645a);border-color:var(--kiosk-line,#d9ded2);}
.text-action{justify-self:start;background:none;color:var(--kiosk-ink,#26382d);border:0;padding:.5rem 0;font-size:.875rem;text-decoration:underline;text-underline-offset:.2em;}
.loan-person{display:grid;gap:.25rem;}.loan-person strong{font-weight:600;}.loan-person .quiet{font-size:.875rem;}
.suggestions{list-style:none;padding:0;border-block:1px solid var(--kiosk-line,#d9ded2);}
.suggestions li+li{border-top:1px solid var(--kiosk-line,#d9ded2);}
.suggestions button{display:grid;gap:.125rem;width:100%;border:0;background:transparent;color:var(--kiosk-ink,#26382d);padding:.625rem .5rem;text-align:left;font-size:1rem;}
.suggestions button:hover:not(:disabled){background:color-mix(in srgb,var(--kiosk-green,#315440) 6%,transparent);}
.suggestions small{font-size:.875rem;color:var(--kiosk-muted,#59645a);overflow-wrap:anywhere;}
.field-note,.error,.success{font-size:.875rem;line-height:1.5;overflow-wrap:anywhere;}
.error{color:#913e30;}.success{color:var(--kiosk-green,#315440);}.field-note{color:var(--kiosk-muted,#59645a);}
.load-error{display:grid;gap:.25rem;}.pending-copy{font-size:.875rem;}
.compact{grid-template-columns:minmax(9rem,.6fr) minmax(0,1.8fr);align-items:start;gap:1rem 1.75rem;}
.compact .copy-status{padding-top:.125rem;gap:.75rem;}
.compact .availability{gap:.5rem;}
.compact .availability strong::before{display:none;}
.compact .home-shelf,.compact .field,.compact .field-note,.compact .error,.compact .success,.compact .pending-copy,.compact .text-action,.compact .loan-person .quiet,.compact .suggestions small{font-size:1rem;}
.compact .field,.task-heading{font-weight:600;}
.compact .lend-row,.compact .return-row{grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:.75rem;}
.compact .return-row{min-height:5.5rem;}
.compact .return-details{align-self:stretch;align-content:space-between;gap:.5rem;}
.compact input,.compact .primary{min-height:4rem;}
.compact .primary{min-width:10rem;width:auto;padding:1rem 1.25rem;}
.compact .borrower-shortcuts{display:flex;flex-wrap:wrap;gap:.5rem;}
.borrower-shortcuts button{min-height:3rem;max-width:100%;padding:.625rem .875rem;border:1px solid var(--kiosk-line,#d9ded2);background:var(--kiosk-paper,#fafbf7);color:var(--kiosk-ink,#26382d);font-size:1rem;overflow-wrap:anywhere;}
.borrower-shortcuts button:hover:not(:disabled){background:color-mix(in srgb,var(--kiosk-green,#315440) 8%,var(--kiosk-paper,#fafbf7));border-color:var(--kiosk-green,#315440);}
.compact>p,.compact>.primary,.compact>.load-error{grid-column:1/-1;}
@media(max-width:760px){
  .compact{grid-template-columns:1fr;gap:1rem;}
  .compact .copy-status{padding-top:0;}
  .compact .availability{display:flex;flex-wrap:wrap;gap:.25rem 1rem;}
}
@media(max-width:400px){
  .compact .lend-row,.compact .return-row{grid-template-columns:1fr;}
  .compact .primary{width:100%;}
}
.compact{grid-template-columns:1fr;gap:26px}.compact .availability{gap:8px}.compact .home-shelf{font-size:22px;color:var(--kiosk-ink);font-weight:500}.compact .lend-row,.compact .return-row{grid-template-columns:1fr;gap:16px}.compact .primary{width:100%}.compact .field{gap:10px}.compact input{min-height:56px}.compact .return-details{gap:14px}.compact .availability strong{font-size:16px}.compact .borrower-shortcuts{gap:8px}.compact .borrower-shortcuts button{padding:8px 10px;font-size:15px}
.compact .home-shelf{display:grid;gap:5px}.compact .home-shelf small{font-size:14px;color:var(--kiosk-muted);font-weight:400}
.compact{grid-template-columns:minmax(150px,.65fr) minmax(0,1.8fr);gap:24px}.compact .lend-row,.compact .return-row{grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:end}.compact .primary{width:auto;min-width:142px;min-height:56px}.compact .home-shelf{font-size:21px}.compact .home-shelf small{display:none}.compact .availability{gap:10px}.compact .availability strong{display:inline-flex;justify-self:start;padding:5px 9px;border-radius:3px;background:var(--app-soft);font-size:16px}.compact .availability strong.out{background:#f3e4cd;color:#714514}.compact .borrower-shortcuts{gap:6px}.compact .borrower-shortcuts button{min-height:44px}
@media(max-width:1000px){.compact{grid-template-columns:1fr;gap:14px}.compact .availability{display:flex;align-items:center;gap:16px}.compact .home-shelf{font-size:18px}}
@media(max-width:700px){.compact .lend-row,.compact .return-row{grid-template-columns:1fr;gap:14px}.compact .primary{width:100%}.compact .availability{display:grid;gap:10px}.compact .home-shelf{font-size:22px}}
.quick-lend{display:grid;gap:12px}.compact .quick-lend .borrower-shortcuts{flex-wrap:wrap;gap:10px}.compact .quick-lend .borrower-shortcuts button{min-height:56px;font-size:17px;padding:12px 18px;border-color:var(--kiosk-green);background:var(--kiosk-green);color:white}.compact .quick-lend .borrower-shortcuts .new-person{background:var(--kiosk-paper);color:var(--kiosk-green);border-color:var(--kiosk-line)}
.compact{grid-template-columns:260px minmax(0,1fr);gap:28px}.compact .copy-status{align-self:center}.compact .availability strong{padding:0;background:none;font-size:18px;font-weight:600}.compact .home-shelf{font-size:20px}.compact .home-shelf small{display:inline;font-size:15px}.compact .home-shelf{display:flex;gap:8px;align-items:baseline}.compact .availability strong.out{background:none;color:#714514}.compact .task-heading,.compact .field{font-size:18px}.compact .primary{font-size:18px;min-width:168px}.compact input{font-size:18px}.compact .return-row{min-height:80px}.compact .availability{gap:8px}
@media(max-width:1000px){.compact{grid-template-columns:200px minmax(0,1fr);gap:24px}.compact .availability{display:grid;gap:8px}}
@media(max-width:700px){.compact{grid-template-columns:1fr;gap:22px}.compact .quick-lend .borrower-shortcuts{display:grid;grid-template-columns:1fr}.compact .return-row{grid-template-columns:1fr}}
.compact{grid-template-columns:360px minmax(0,1fr);gap:28px}.compact .copy-status{display:grid;grid-template-columns:68px minmax(0,1fr);gap:6px 16px;align-items:center}.selected-thumb{grid-column:1;grid-row:1/4;display:block;width:68px;max-height:106px;height:auto;object-fit:contain;box-shadow:1px 3px 5px #22291f1a}.no-thumb{height:96px;background:#e7e1d3;display:grid;place-items:center;font:400 30px Newsreader,Georgia,serif;color:var(--kiosk-muted)}.current-book{grid-column:2;color:var(--kiosk-ink);text-decoration:none;min-height:48px;display:grid;align-content:center;gap:4px}.current-book strong{font:500 24px/1.05 Newsreader,Georgia,serif}.current-book span{font-size:16px;color:var(--kiosk-muted)}.current-book:hover strong{text-decoration:underline;text-underline-offset:4px}.compact .copy-choice,.compact .availability{grid-column:2}.compact .availability{gap:2px}.compact .availability strong{color:var(--kiosk-ink);font-size:16px}.compact .home-shelf{font-size:17px;gap:6px}.compact .home-shelf small{font-size:15px}.compact .success{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
@media(max-width:1000px){.compact{grid-template-columns:310px minmax(0,1fr)}}
@media(max-width:700px){.compact{grid-template-columns:1fr;gap:24px}.compact .copy-status{grid-template-columns:68px minmax(0,1fr)}.compact .availability{gap:4px}}
.compact{grid-template-columns:1fr;gap:26px}.compact .copy-status{grid-template-columns:126px minmax(0,1fr);gap:12px 18px;align-items:center}.selected-thumb{width:126px;height:auto;max-height:190px;box-shadow:1px 6px 9px #22291f20}.no-thumb{height:188px}.current-book strong{font-size:29px;line-height:1.04}.current-book span{font-size:18px}.current-book{align-self:end}.compact .availability{gap:8px;align-self:start}.compact .availability strong{font-size:18px}.compact .home-shelf{display:grid;gap:3px;font-size:18px}.compact .home-shelf small{font-size:16px}.compact .lend-row,.compact .return-row{display:grid;grid-template-columns:1fr;gap:18px;align-items:stretch}.compact .return-row{min-height:0}.compact .return-details{gap:14px}.compact .loan-person{font-size:20px}.compact .primary{width:100%;min-height:64px;font-size:20px}.compact .field{font-size:19px;gap:12px}.compact input{min-height:60px}.compact .quick-lend .borrower-shortcuts{display:grid;grid-template-columns:1fr}.compact .quick-lend .borrower-shortcuts button{min-height:56px;font-size:18px}.compact .circulation-actions{border-top:1px solid var(--app-line);padding-top:24px}
@media(max-width:1100px){.compact .copy-status{grid-template-columns:106px minmax(0,1fr);gap:10px 16px}.selected-thumb{width:106px;max-height:160px}.current-book strong{font-size:27px}.current-book span{font-size:16px}.compact .home-shelf{font-size:17px}}
@media(max-width:700px){.compact .copy-status{grid-template-columns:94px minmax(0,1fr)}.selected-thumb{width:94px;max-height:142px}.current-book strong{font-size:30px}.compact .quick-lend .borrower-shortcuts{grid-template-columns:1fr}.compact .primary{min-height:60px}}

.compact{display:grid;grid-template-columns:minmax(240px,350px) minmax(0,1fr);grid-template-rows:auto auto 1fr;column-gap:64px;row-gap:18px;align-items:start;width:100%;max-width:1120px;margin-inline:auto}.compact .copy-status{display:contents}.selected-thumb{grid-column:1;grid-row:1/4;width:auto;max-width:100%;max-height:min(440px,calc(100svh - 250px));height:auto;justify-self:center;align-self:center;box-shadow:2px 12px 20px #22291f28}.no-thumb{width:290px;height:420px;max-width:100%;padding:32px;display:flex;flex-direction:column;align-items:start;justify-content:space-between;text-align:left;font:400 36px/1.1 Newsreader,Georgia,serif}.no-thumb small{font:400 18px/1.4 'Library Inter',sans-serif}.current-book{grid-column:2;grid-row:1;min-height:0;align-self:start;gap:12px}.current-book strong{font-size:clamp(36px,4vw,52px);line-height:1.05;letter-spacing:-.025em}.current-book span{font-size:21px;color:#465247}.compact .availability{grid-column:2;grid-row:2;display:flex;flex-wrap:wrap;align-items:center;gap:12px 22px;padding:12px 0 18px;border-bottom:1px solid var(--app-line)}.compact .availability strong{font-size:20px}.compact .home-shelf{display:flex;gap:8px;font-size:20px}.compact .home-shelf small{font-size:18px}.compact .circulation-actions{grid-column:2;grid-row:3;border:0;padding:0;max-width:580px}.compact .return-details{gap:12px}.compact .loan-person{font-size:24px;gap:8px}.compact .loan-person .quiet{font-size:18px;color:#465247}.compact .return-row{gap:24px}.compact .primary{min-height:66px;font-size:21px}.compact .quick-lend .borrower-shortcuts{display:flex;flex-wrap:wrap;gap:12px}.compact .quick-lend .borrower-shortcuts button{min-height:64px;font-size:20px;padding:14px 24px;flex:1}.compact .quick-lend .borrower-shortcuts .new-person{flex-basis:100%;background:none;border:0;justify-self:start;min-height:48px;padding:8px;font-size:18px}.compact .task-heading,.compact .field{font-size:20px}.compact input{font-size:20px;min-height:62px}.compact .lend-row{gap:16px}.compact>.error,.compact>.load-error,.compact>.field-note{grid-column:2}.compact .copy-choice{grid-column:2;grid-row:2}
@media(max-width:1000px){.compact{grid-template-columns:minmax(180px,260px) minmax(0,1fr);gap:18px 32px}.current-book strong{font-size:40px}.current-book span{font-size:19px}.compact .availability strong,.compact .home-shelf{font-size:18px}.compact .loan-person{font-size:22px}}
@media(max-width:700px){.compact{display:grid;grid-template-columns:96px minmax(0,1fr);gap:16px 20px}.selected-thumb{grid-column:1;grid-row:1/3;width:96px;max-height:146px;align-self:start}.no-thumb{font-size:18px;padding:12px;height:146px}.no-thumb small{font-size:10px}.current-book{grid-column:2;grid-row:1;gap:8px}.current-book strong{font-size:30px}.current-book span{font-size:17px}.compact .availability{grid-column:2;grid-row:2;display:grid;gap:6px;padding:0;border:0}.compact .availability strong,.compact .home-shelf{font-size:17px}.compact .home-shelf small{font-size:15px}.compact .circulation-actions{grid-column:1/-1;grid-row:3;max-width:none;border-top:1px solid var(--app-line);padding-top:22px}.compact .loan-person{font-size:22px}.compact .primary{min-height:60px}.compact .quick-lend .borrower-shortcuts{display:grid;grid-template-columns:1fr}.compact>.error,.compact>.load-error,.compact>.field-note{grid-column:1/-1}}
.compact:has(.copy-choice){grid-template-rows:auto auto auto 1fr}.compact:has(.copy-choice) .selected-thumb{grid-row:1/5}.compact:has(.copy-choice) .copy-choice{grid-column:2;grid-row:2}.compact:has(.copy-choice) .availability{grid-row:3}.compact:has(.copy-choice) .circulation-actions{grid-row:4}@media(max-width:700px){.compact:has(.copy-choice) .selected-thumb{grid-row:1/3}.compact:has(.copy-choice) .copy-choice{grid-column:1/-1;grid-row:3}.compact:has(.copy-choice) .availability{grid-row:2}.compact:has(.copy-choice) .circulation-actions{grid-row:4}}
.compact .availability{max-width:580px}

.compact{max-width:none;margin:0;grid-template-columns:minmax(280px,35%) minmax(0,1fr);column-gap:48px;row-gap:18px;min-height:min(520px,calc(100svh - 264px))}.selected-thumb{justify-self:start;max-height:min(520px,calc(100svh - 264px));box-shadow:3px 9px 8px #22291f25}.current-book strong{font-size:48px}.current-book span{font-size:22px}.compact .availability{max-width:none;border:0;padding:8px 0 10px;gap:24px;align-items:center}.compact .availability strong{padding:8px 14px;border-radius:4px;font-size:20px;background:#e0e8dd;color:#2c4532}.compact .availability strong.out{background:#eeddbb;color:#573916}.compact .home-shelf{display:grid;gap:4px;font-size:22px}.compact .home-shelf small{font-size:18px;color:#465247}.compact .circulation-actions{max-width:none;align-self:end}.compact .loan-person .quiet{font-size:20px;color:#465247}.compact .primary{border-radius:4px}.compact .quick-lend .borrower-shortcuts button{border-radius:4px}.compact input{border-radius:4px}.compact .return-row{gap:28px}.compact .loan-person{font-size:25px;gap:8px}.compact .field{font-size:20px}
@media(max-width:1000px){.compact{grid-template-columns:minmax(200px,32%) minmax(0,1fr);column-gap:32px}.current-book strong{font-size:38px}.current-book span{font-size:20px}.compact .home-shelf{font-size:20px}.compact .availability{gap:18px}.compact .availability strong{font-size:18px}.compact .loan-person{font-size:22px}}
@media(max-width:700px){.compact{grid-template-columns:96px minmax(0,1fr);gap:16px 20px;min-height:0}.selected-thumb{width:96px;max-height:146px}.current-book strong{font-size:29px}.current-book span{font-size:17px}.compact .availability{padding:0;gap:10px}.compact .availability strong{font-size:16px;padding:5px 8px;justify-self:start}.compact .home-shelf{font-size:17px}.compact .home-shelf small{font-size:15px}.compact .loan-person{font-size:22px}.compact .loan-person .quiet{font-size:17px}.compact .circulation-actions{align-self:start}.compact .primary{min-height:60px}}
</style>
