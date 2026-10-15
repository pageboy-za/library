#!/usr/bin/env python3
"""Prepare/apply a private fill-only metadata plan from the coverage audit cache.

No provider requests are made. Keep the cache, plan and results outside Git.
"""
import argparse
import datetime
import json
import os
import pathlib
import re
import unicodedata
import urllib.parse
import urllib.request


def request(base, path, body=None):
    headers = {}
    if body is not None:
        headers = {'Content-Type': 'application/json', 'Origin': base}
    req = urllib.request.Request(base + path, headers=headers,
                                 data=None if body is None else json.dumps(body).encode())
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            raise ValueError('Unexpected local API redirect')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with opener.open(req, timeout=60) as response:
        raw = response.read((16 << 20) + 1)
    if len(raw) > 16 << 20:
        raise ValueError('Oversized local response')
    return json.loads(raw)


def snapshot(base):
    books = []
    for page in range(1, 10000):
        response = request(base, '/api/books?page=' + str(page))
        books += response['books']
        if len(books) >= response['total']:
            break
        if not response['books']:
            raise ValueError('Incomplete catalogue pagination')
    else:
        raise ValueError('Too many catalogue pages')
    active = {b['id'] for b in books}
    books += request(base, '/api/deleted-books')
    return {'books': books, 'active_ids': sorted(active), 'other': {
        path: request(base, path) for path in ('/api/shelves', '/api/bookcases', '/api/borrowers', '/api/loans', '/api/preferences')}, 'discovery': {
        str(b['id']): request(base, '/api/books/' + str(b['id']) + '/discovery')
        for b in books}}


def labels(values):
    if not isinstance(values, list):
        return []
    out, seen = [], set()
    for value in values:
        if not isinstance(value, str):
            continue
        value = value.strip()
        key = unicodedata.normalize("NFKC", value).lower()
        if (not value or len(value.encode()) > 200 or key in seen
                or key in {'accessible book', 'protected daisy', 'in library', 'internet archive wishlist'}
                or key.startswith(('nyt:', 'reading_level:', 'collection:', 'open_syllabus:'))):
            continue
        seen.add(key)
        out.append(value)
        if len(out) == 100:
            break
    return out


def load_cache(directory):
    editions, works = {}, {}
    for path in sorted(directory.glob('*.json')):
        if path.stat().st_size > 16 << 20:
            raise ValueError('Oversized cache entry')
        response = json.loads(path.read_text())
        for key, item in response.items():
            if re.fullmatch(r'OLID:OL[0-9]+M', key) and isinstance(item, dict):
                edition = item.get('details', {})
                if edition.get('key') == '/books/' + key[5:]:
                    editions[key[5:]] = edition
        for work in response.get('docs', []):
            key = work.get('key', '')
            key = key if key.startswith('/works/') else '/works/' + key
            if re.fullmatch(r'/works/OL[0-9]+W', key):
                works[key] = work
    return editions, works


def prepare(before, editions, works):
    updates, uncached = [], 0
    for book in before['books']:
        if book['id'] not in before['active_ids']:
            continue
        edition = editions.get(book.get('source_id', ''))
        if edition is None:
            uncached += 1
            continue
        # Use only the exact edition's explicit single work relationship.
        refs = edition.get('works', [])
        work_key = refs[0].get('key', '') if len(refs) == 1 and isinstance(refs[0], dict) else ''
        work = works.get(work_key, {})
        subjects = labels(edition.get('subjects')) or labels(work.get('subject'))
        year = work.get('first_publish_year')
        if type(year) is not int or not 1 <= year <= datetime.date.today().year + 1:
            year = None
        discovery = before['discovery'][str(book['id'])]
        subjects = subjects if not discovery.get('subjects') else []
        year = year if not book.get('first_published_year') and not book.get('bibliography_revision', 0) else None
        if not subjects and year is None:
            continue
        updates.append({k: book.get(k, '') for k in ('id', 'isbn', 'source_id', 'revision')} | {
            'discovery_revision': discovery['revision'],
            'bibliography_revision': book.get('bibliography_revision', 0),
            'subjects': subjects, 'first_published_year': year,
            'first_published_year_source_url': 'https://openlibrary.org' + work_key if year else ''})
    return {'format': 1, 'before': before, 'updates': updates, 'uncached_books': uncached}


def verify(plan, after):
    before = plan['before']
    current = {b['id']: b for b in after['books']}
    expected = {u['id']: u for u in plan['updates']}
    if set(current) != {b['id'] for b in before['books']} or before['active_ids'] != after['active_ids']:
        raise ValueError('Catalogue membership changed')
    if before['other'] != after['other']:
        raise ValueError('Shelves, borrowers, loans or preferences changed')
    added_subjects = added_years = 0
    allowed = {'subjects', 'first_published_year', 'first_published_year_source_url', 'bibliography_revision'}
    for old in before['books']:
        new = current[old['id']]
        if any(new.get(k) != v for k, v in old.items() if k not in allowed):
            raise ValueError('Unrelated book or inventory details changed')
        d = before['discovery'][str(old['id'])]
        nd = after['discovery'][str(old['id'])]
        if any(nd.get(k) != v for k, v in d.items() if k not in {'subjects', 'revision'}):
            raise ValueError('Private discovery details changed')
        u = expected.get(old['id'], {})
        if nd.get('subjects') != d.get('subjects'):
            if d.get('subjects') or nd.get('subjects') != u.get('subjects'):
                raise ValueError('Unexpected subject changes')
            added_subjects += 1
        elif nd['revision'] != d['revision']:
            raise ValueError('Unexpected discovery revision')
        if new.get('first_published_year') != old.get('first_published_year'):
            if (old.get('first_published_year') or old.get('bibliography_revision', 0)
                    or new.get('first_published_year') != u.get('first_published_year')
                    or new.get('first_published_year_source_url') != u.get('first_published_year_source_url')):
                raise ValueError('Unexpected publication year changes')
            added_years += 1
        elif any(new.get(k, 0 if k == 'bibliography_revision' else '') != old.get(k, 0 if k == 'bibliography_revision' else '')
                 for k in ('bibliography_revision', 'first_published_year_source_url')):
            raise ValueError('Unexpected publication provenance changes')
    return {'subjects_added': added_subjects, 'years_added': added_years,
            'other_book_inventory_and_private_details_preserved': True}


def batches(updates):
    group = []
    for update in updates:
        if group and (len(group) == 20 or len(json.dumps({'updates': group + [update]}).encode()) > 60000):
            yield group
            group = []
        group.append(update)
    if group:
        yield group


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default='http://127.0.0.1:8080')
    parser.add_argument('--cache', type=pathlib.Path)
    parser.add_argument('--plan', type=pathlib.Path, required=True)
    parser.add_argument('--apply', action='store_true')
    args = parser.parse_args()
    origin = urllib.parse.urlsplit(args.base)
    if (origin.scheme != 'http' or origin.hostname not in ('127.0.0.1', 'localhost')
            or origin.username or origin.password or origin.path or origin.query or origin.fragment):
        parser.error('Use the private loopback API origin')
    os.umask(0o077)
    if not args.apply:
        if not args.cache:
            parser.error('--cache is required when preparing a plan')
        plan = prepare(snapshot(args.base), *load_cache(args.cache))
        with args.plan.open('x') as output:
            json.dump(plan, output)
            output.flush()
            os.fsync(output.fileno())
        print(json.dumps({'dry_run': True, 'active_books': len(plan['before']['active_ids']),
                          'planned_books': len(plan['updates']), 'uncached_books': plan['uncached_books'],
                          'planned_subjects': sum(bool(u['subjects']) for u in plan['updates']),
                          'planned_years': sum(u['first_published_year'] is not None for u in plan['updates'])}))
    else:
        if args.plan.stat().st_size > 32 << 20:
            raise ValueError('Oversized plan')
        plan = json.loads(args.plan.read_text())
        if plan['format'] != 1:
            raise ValueError('Unsupported plan')
        totals = {k: 0 for k in ('changed', 'subjects_changed', 'years_changed', 'skipped')}
        for group in batches(plan['updates']):
            result = request(args.base, '/api/metadata/backfill', {'updates': group})
            for key in totals:
                totals[key] += result[key]
        after = snapshot(args.base)
        outcome = verify(plan, after) | totals
        with pathlib.Path(str(args.plan) + '.result.json').open('w') as output:
            json.dump(outcome, output)
        print(json.dumps(outcome))


if __name__ == '__main__':
    main()
