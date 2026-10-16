<script lang="ts">
import {onMount} from 'svelte';
import {page} from '$app/state';
import {loadSession,session} from '../../lib/auth';
let {children}=$props();
let ready=$state(false);let error=$state('');
const login=$derived(page.url.pathname==='/staff/login/');
const adminOnly=$derived(['/staff/settings/','/staff/accounts/'].some(path=>page.url.pathname.startsWith(path)));
async function check(){ready=false;error='';try{await loadSession(true);ready=true;}catch(e){error=(e as Error).message;}}
onMount(()=>{void check();});
</script>
{#if login}
 {@render children()}
{:else if error}
 <h1>Could not check your sign-in</h1><p role="alert">{error}</p><button class="library-button" onclick={check}>Try again</button>
{:else if !ready}
 <p role="status">Checking your sign-in…</p>
{:else if !$session?.user}
 <h1>Staff sign-in required</h1><p>Sign in to manage books, members and lending.</p>
 {#if $session?.setup_required}<p>The first administrator must be created on the library server. Ask the person who installed this library to complete setup.</p>{:else}<a class="library-button" href={`/staff/login/?next=${encodeURIComponent(page.url.pathname+page.url.search)}`}>Staff sign in</a>{/if}
 <p><a href="/">Browse the library</a></p>
{:else if adminOnly&&$session.user.role!=='administrator'}
 <h1>Administrator access required</h1><p>Your librarian account can manage books and lending. An administrator manages accounts, updates and settings.</p><a href="/staff/">Back to staff library</a>
{:else}
 {@render children()}
{/if}
