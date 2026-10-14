<script lang="ts">
import {onMount} from 'svelte';
import {api} from '../../lib/api';
type BackupHealth={configured:boolean;state:'unconfigured'|'no_backups'|'verified'|'failed';last_verified_at?:string;last_attempt_at?:string;backup_count:number;message:string;stale:boolean;checked_at:string};
let health:BackupHealth|null=null;let busy=false;let error='';
function date(value?:string){if(!value)return 'Not yet';const parsed=new Date(value);return Number.isNaN(parsed.getTime())?'Not available':parsed.toLocaleString();}
function label(value:BackupHealth){if(!value.configured||value.state==='unconfigured')return 'Backups are not set up';if(value.state==='failed')return 'A backup needs attention';if(value.state==='no_backups')return 'No verified backup yet';return value.stale?'Your latest backup is over two days old':'A verified backup is available';}
async function refresh(){if(busy)return;busy=true;error='';try{health=await api<BackupHealth>('/api/backup-status');}catch{error='Backup status could not be checked. Try again.';}finally{busy=false;}}
onMount(()=>{refresh();});
</script>
<section class="backup-status_wrap" aria-labelledby="backup-heading" aria-busy={busy}>
<div class="library-actions"><h2 id="backup-heading">Backups</h2><button type="button" class="library-button secondary" disabled={busy} onclick={refresh}>{busy?'Checking…':'Refresh status'}</button></div>
{#if error}<p role="alert" class="library-error">{error}</p>{/if}
{#if busy&&!health}<p role="status">Checking your saved backups…</p>{/if}
{#if health}
<div class="backup-status_card"><p role="status" class="backup-status_summary">{label(health)}</p><p>{health.message}</p>
{#if health.configured}
<dl><dt>Saved backup folders</dt><dd>{health.backup_count}</dd><dt>Latest verified backup</dt><dd>{date(health.last_verified_at)}</dd><dt>Latest saved attempt</dt><dd>{date(health.last_attempt_at)}</dd></dl>
{#if health.stale&&health.last_verified_at}<p class="library-notice">Changes made since the last verified backup may not be protected. Check your backup disk and daily backup setup.</p>{/if}
{#if health.state==='failed'}<p class="library-help">Check that your backup disk is connected and has enough space. Any earlier verified backup is listed above.</p>{/if}
{:else}<p>Choose a durable external backup disk and a folder for your backups. Then set up daily backups and tell the library which folder to check.</p>
<details><summary>Help with setup</summary><p class="library-help">The setup guide explains the daily backup helper. When starting the library, the optional <code>-backup-dir</code> setting tells this screen where to look for saved backups.</p></details>{/if}
<p class="library-help">Status checked {date(health.checked_at)}{error?' · This result may be out of date.':''}</p></div>
{/if}
<p class="library-help">This screen checks saved backup folders. It does not check the daily schedule or the health of your disk, and does not create, delete or schedule backups. Keep a backup on another disk so it remains available if this device fails.</p>
</section>
<style>@layer components{.backup-status_wrap{display:grid;gap:var(--space-4);max-width:46rem;h2{font-family:Georgia,serif;font-size:var(--h4)}}.backup-status_card{display:grid;gap:var(--space-3);padding:var(--space-4);border:var(--border-width) solid var(--border);border-radius:var(--radius-main);background:var(--background-3);dl{display:grid;grid-template-columns:minmax(8rem,auto) 1fr;gap:var(--space-2) var(--space-4);dd{overflow-wrap:anywhere}}details{display:grid;gap:var(--space-2);summary{cursor:pointer}}}.backup-status_summary{font-weight:600}}</style>
