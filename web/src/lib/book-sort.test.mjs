import assert from 'node:assert/strict';
import {test} from 'node:test';
import {compareBooks} from './book-sort.ts';

const books = [
  {id: 1, title: 'Paper Planes', subtitle: 'The Hidden Patterns of Folding', authors: ['Example Writer'], first_published_year: 2020},
  {id: 2, title: 'The Hidden Door', subtitle: 'A remarkable true story', authors: ['Alex Author'], first_published_year: 2002},
  {id: 3, title: 'The Hidden Life', subtitle: '', authors: ['Zoe'], first_published_year: 1990},
  {id: 4, title: 'Another book', subtitle: '', authors: ['The Historian'], first_published_year: null},
];
const ordered = (sort, query) => [...books].sort((a, b) => compareBooks(a, b, sort, query)).map(book => book.id);

test('title matches precede subtitle and author matches regardless of selected sort', () => {
  for (const sort of ['title', 'title_desc', 'author', 'author_desc', 'first_published_newest', 'first_published_oldest']) {
    const ids = ordered(sort, '  THE H  ');
    assert.ok(ids.indexOf(2) < ids.indexOf(1), sort);
    assert.ok(ids.indexOf(3) < ids.indexOf(1), sort);
    assert.ok(ids.indexOf(2) < ids.indexOf(4), sort);
  }
});

test('selected sort is preserved within title matches and other matches', () => {
  assert.deepEqual(ordered('title', 'the h'), [2, 3, 4, 1]);
  assert.deepEqual(ordered('title_desc', 'the h'), [3, 2, 1, 4]);
  assert.deepEqual(ordered('first_published_newest', 'the h'), [2, 3, 1, 4]);
});

test('clearing the search restores ordinary sorting', () => {
  assert.deepEqual(ordered('title', ''), [4, 1, 2, 3]);
  assert.deepEqual(ordered('first_published_newest', ' '), [1, 2, 3, 4]);
});
