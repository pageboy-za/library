# Book subtitles

Subtitles are optional, editable details beneath the title in book, catalogue and kiosk views. ISBN lookup captures Open Library's separate subtitle field. Search includes subtitles. Titles are never split or rewritten to guess a subtitle.

Startup adds `books.subtitle` with an empty default inside the existing transactional migration and rebuilds derived search data. Existing records, copies, shelves, loans and covers are retained. Backups include subtitles. Older app versions retain the column but do not display it; clients that omit the field do not clear it in this version.

## Backfill existing records

The owner must approve sending existing ISBNs or edition IDs to Open Library. Notes, titles, borrower information, shelves and credentials are never sent. Unavailable or unmatched subtitles remain blank for manual entry.

After taking a verified catalogue backup, use the owner's existing Pi terminal. No administrator access is required:

```
python3 backfill-subtitles.py --plan ~/subtitle-plan.json
python3 backfill-subtitles.py --plan ~/subtitle-plan.json --apply
```

The first command only collects a private plan and prints counts. It includes active and deleted books, uses exact identifiers, bounds response sizes, restricts redirects, and paces provider requests. It does not overwrite an existing plan. The second fills only blank subtitles whose revision and edition identity still match. It preserves existing subtitles and skips concurrent edits. It changes no copies or deletion state, and publishes pages once for the batch. A retry safely skips completed fills and retries publication; create a fresh plan to retry provider failures or skipped edits.

Keep the plan private: it contains catalogue records for preservation checks and is created with owner-only permissions. Never commit it. The final report checks other metadata and inventory against that plan; concurrent user edits require separate review rather than overwriting them. Keep book entry paused during installation and application of the plan for an unambiguous check.
