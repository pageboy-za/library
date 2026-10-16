<script lang="ts">
import {page} from '$app/state';
import {afterNavigate,goto} from '$app/navigation';
import {session,signOut} from '../../lib/auth';
import LibraryMark from '../library/LibraryMark.svelte';
let more=$state<HTMLDetailsElement>();let busy=$state(false);let error=$state('');
const staff=$derived(page.url.pathname.startsWith('/staff/')&&page.url.pathname!=='/staff/login/');
const secondaryActive=$derived(['/staff/discover','/staff/shelves','/staff/borrowers','/staff/members','/staff/settings','/staff/accounts','/staff/account'].some(path=>page.url.pathname.startsWith(path)));
const current=(href:string)=>page.url.pathname.replace(/\/+$/,'')===href.replace(/\/+$/,'')?'page':undefined;
afterNavigate(()=>{if(more)more.open=false;});
function outside(event:MouseEvent){if(more?.open&&event.target instanceof Node&&!more.contains(event.target))more.open=false;}
function escape(event:KeyboardEvent){if(event.key==='Escape'&&more?.open){more.open=false;more.querySelector('summary')?.focus();}}
async function logout(){busy=true;error='';try{await signOut();await goto('/staff/login/');}catch(e){error=(e as Error).message;}finally{busy=false;}}
</script>
<svelte:document onclick={outside} onkeydown={escape}/>
<nav class="app-nav" aria-label={staff?'Staff navigation':'Library navigation'}>
<ul class="app-nav_primary">
<li><a class="nav_link" href={staff?'/staff/':'/'} aria-current={current(staff?'/staff/':'/')}><LibraryMark/>{staff?'Staff library':'Library'}</a></li>
{#if staff&&$session?.user}
<li><a class="nav_link" href="/staff/loans/" aria-current={current('/staff/loans/')}>Loans &amp; returns</a></li>
<li><a class="nav_link" href="/staff/scan/" aria-current={current('/staff/scan/')}>Add book</a></li>
{:else}
<li><a class="nav_link" href="/join/" aria-current={current('/join/')}>Join library</a></li><li><a class="nav_link" href="/membership/" aria-current={current('/membership/')}>Your library card</a></li>
<li><a class="nav_link" href={$session?.user?'/staff/':'/staff/login/'}>{$session?.user?'Staff area':'Staff sign in'}</a></li>
{/if}
</ul>
{#if staff&&$session?.user}<details bind:this={more} class:is-active={secondaryActive} class="app-nav_more"><summary>More</summary><ul class="app-nav_menu" aria-label="Staff tools">
<li><a class="nav_link" href="/staff/catalogue/" aria-current={current('/staff/catalogue/')}>Staff catalogue</a></li><li><a class="nav_link" href="/staff/discover/" aria-current={current('/staff/discover/')}>Discover by theme</a></li><li><a class="nav_link" href="/staff/shelves/" aria-current={current('/staff/shelves/')}>Shelves</a></li><li><a class="nav_link" href="/staff/members/" aria-current={current('/staff/members/')}>Members</a></li><li><a class="nav_link" href="/staff/borrowers/" aria-current={current('/staff/borrowers/')}>Borrowers</a></li>
{#if $session.user.role==='administrator'}<li><a class="nav_link" href="/staff/settings/" aria-current={current('/staff/settings/')}>Settings &amp; backups</a></li><li><a class="nav_link" href="/staff/accounts/" aria-current={current('/staff/accounts/')}>Staff accounts</a></li>{/if}
<li><a class="nav_link" href="/staff/account/" aria-current={current('/staff/account/')}>Your account</a></li><li><a class="nav_link" href="/">Member view</a></li><li><button class="nav_link" disabled={busy} onclick={logout}>{busy?'Signing out…':'Sign out'}</button></li>
</ul></details>{/if}
</nav>
{#if error}<p role="alert" class="library-error">{error}</p>{/if}
<style>

@layer components {
.app-nav_menu button.nav_link{font:inherit;background:none;border:0;width:100%;text-align:left;cursor:pointer}
.app-nav{position:fixed;inset:0 0 auto;z-index:50;display:flex;justify-content:flex-start;align-items:stretch;gap:1rem;min-height:4rem;padding:.4rem clamp(1.25rem,3vw,3rem);border-bottom:1px solid var(--app-line);background:var(--app-paper);color:var(--app-ink)}
.app-nav_primary{display:flex;align-items:stretch;gap:1rem;list-style:none;padding:0;margin:0}.app-nav_primary li{display:flex}
.app-nav_primary .nav_link,.app-nav_more>summary{display:flex;align-items:center;justify-content:center;gap:.7rem;min-height:3.25rem;min-width:0;padding:.65rem 1.1rem;font-size:.9rem;line-height:1.2;color:var(--app-muted);text-decoration:none;border:0;border-radius:.3rem;cursor:pointer;list-style:none}
.app-nav_primary .nav_link[aria-current=page],.app-nav_more.is-active>summary{background:transparent;color:var(--app-accent);font-weight:600}.app-nav_primary .nav_link[aria-current=page]{position:relative}.app-nav_primary .nav_link[aria-current=page]::after{content:"";position:absolute;bottom:.15rem;left:3.1rem;right:1.1rem;height:2px;background:var(--app-accent)}.app-nav_primary .nav_link:hover,.app-nav_more>summary:hover{background:var(--app-soft)}
.app-nav :global(svg){width:1.3rem;height:1.3rem;stroke:currentColor;stroke-width:1.5;fill:none;stroke-linecap:round;stroke-linejoin:round;flex-shrink:0}.app-nav_more{position:relative;margin-left:auto}.app-nav_more>summary{min-width:6rem}.app-nav_more>summary::-webkit-details-marker{display:none}
.app-nav_menu{position:absolute;top:calc(100% + 1rem);right:0;width:15rem;padding:.4rem;margin:0;list-style:none;border:1px solid var(--app-line);background:var(--app-surface);border-radius:.4rem;box-shadow:var(--app-shadow)}.app-nav_menu .nav_link{display:flex;align-items:center;min-height:3rem;padding:.65rem .85rem;color:var(--app-ink);text-decoration:none;font-size:.9rem;border-radius:.2rem}.app-nav_menu .nav_link:hover,.app-nav_menu .nav_link[aria-current=page]{background:var(--app-soft)}
@media(width>=48rem){.app-nav_primary li:first-child .nav_link{padding-left:0}.app-nav_primary li:first-child .nav_link[aria-current=page]::after{left:2rem}}
@media(width<48rem){.app-nav_primary .nav_link[aria-current=page]::after{left:50%;right:auto;width:2rem;transform:translateX(-50%)}.app-nav{inset:auto 0 0;justify-content:center;border-top:1px solid var(--app-line);border-bottom:0;gap:0;padding:.3rem .5rem calc(.3rem + env(safe-area-inset-bottom));min-height:4.5rem}.app-nav_primary{display:contents}.app-nav_primary li{flex:1;min-width:0}.app-nav_primary .nav_link,.app-nav_more>summary{flex-direction:column;gap:.35rem;font-size:.7rem;padding:.45rem .2rem;min-width:0;min-height:3.5rem;width:100%;text-align:center}.app-nav_more{flex:.8;min-width:0;margin-left:0}.app-nav_menu{width:14rem;top:auto;bottom:calc(100% + 1rem)}.app-nav :global(svg){width:1.2rem;height:1.2rem}}
}

</style>
