import copy
import importlib.util
import json
import pathlib
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('metadata_backfill', pathlib.Path(__file__).with_name('backfill-metadata.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class MetadataBackfillTests(unittest.TestCase):
    def fixture(self):
        before = {'books': [{'id': 1, 'isbn': '9780140328721', 'source_id': 'OL1M', 'revision': 3,
                             'title': 'Private title', 'publication_date': '2020', 'copies': [{'id': 1}]}],
                  'active_ids': [1], 'other': {'loans': []}, 'discovery': {'1': {
                      'revision': 0, 'subjects': [], 'notes': 'Private note', 'tags': ['Mine'], 'description': 'Local description'}}}
        editions = {'OL1M': {'subjects': ['History', 'history', 'nyt:bestseller'], 'works': [{'key': '/works/OL2W'}]}}
        works = {'/works/OL2W': {'first_publish_year': 1980}}
        return before, editions, works

    def test_exact_link_and_only_requested_fields(self):
        before, editions, works = self.fixture()
        plan = m.prepare(before, editions, works)
        u = plan['updates'][0]
        self.assertEqual(u['subjects'], ['History'])
        self.assertEqual(u['first_published_year'], 1980)
        self.assertEqual(u['first_published_year_source_url'], 'https://openlibrary.org/works/OL2W')
        self.assertNotIn('Private', json.dumps(u))
        editions['OL1M']['works'] = [{'key': '/works/OL3W'}]
        self.assertIsNone(m.prepare(before, editions, works)['updates'][0]['first_published_year'])

    def test_preserves_existing_and_manual_clear(self):
        before, editions, works = self.fixture()
        before['discovery']['1']['subjects'] = ['My subject']
        before['books'][0]['bibliography_revision'] = 1
        self.assertEqual(m.prepare(before, editions, works)['updates'], [])

    def test_cache_requires_exact_edition_key(self):
        with tempfile.TemporaryDirectory() as root:
            path = pathlib.Path(root)
            (path / 'one.json').write_text(json.dumps({'OLID:OL1M': {'details': {'key': '/books/OL9M'}}}))
            self.assertEqual(m.load_cache(path), ({}, {}))

    def test_verification_rejects_unrelated_edits(self):
        before, editions, works = self.fixture()
        plan = m.prepare(before, editions, works)
        after = copy.deepcopy(before)
        self.assertTrue(m.verify(plan, after)['other_book_inventory_and_private_details_preserved'])
        after['books'][0].update(first_published_year=1980, first_published_year_source_url='https://openlibrary.org/works/OL2W', bibliography_revision=1)
        after['discovery']['1'].update(subjects=['History'], revision=1)
        self.assertEqual(m.verify(plan, after)['years_added'], 1)
        for mutation in ('notes', 'copies', 'year', 'other'):
            changed = copy.deepcopy(after)
            if mutation == 'notes': changed['discovery']['1']['notes'] = 'Clobbered'
            elif mutation == 'copies': changed['books'][0]['copies'] = []
            elif mutation == 'year': changed['books'][0]['first_published_year'] = 1990
            else: changed['other']['loans'] = [1]
            with self.assertRaises(ValueError): m.verify(plan, changed)

    def test_bounded_batches(self):
        updates = [{'id': i, 'subjects': ['x' * 200] * 100} for i in range(41)]
        groups = list(m.batches(updates))
        self.assertEqual(sum(map(len, groups)), 41)
        self.assertTrue(all(len(g) <= 20 and len(json.dumps({'updates': g}).encode()) <= 60000 for g in groups))


if __name__ == '__main__':
    unittest.main()
