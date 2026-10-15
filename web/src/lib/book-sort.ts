import type {Book} from './api';

export type BookSort = 'title' | 'title_desc' | 'author' | 'author_desc' | 'first_published_newest' | 'first_published_oldest';
type SortableBook = Pick<Book, 'id' | 'title' | 'authors' | 'first_published_year'>;
const collator = new Intl.Collator('en', {sensitivity: 'base', numeric: true});

export function compareBooks(a: SortableBook, b: SortableBook, sort: BookSort, query = ''): number {
  const search = query.trim().toLowerCase();
  if (search) {
    const titleMatchOrder = Number(!a.title.toLowerCase().includes(search)) - Number(!b.title.toLowerCase().includes(search));
    if (titleMatchOrder) return titleMatchOrder;
  }
  const titleOrder = collator.compare(a.title, b.title) || a.id - b.id;
  if (sort === 'title_desc') return -titleOrder;
  if (sort === 'author' || sort === 'author_desc') {
    const authorA = a.authors[0]?.trim() || '';
    const authorB = b.authors[0]?.trim() || '';
    if (!authorA || !authorB) return Number(!authorA) - Number(!authorB) || titleOrder;
    return collator.compare(authorA, authorB) * (sort === 'author_desc' ? -1 : 1) || titleOrder;
  }
  if (sort === 'first_published_newest' || sort === 'first_published_oldest') {
    const yearA = a.first_published_year;
    const yearB = b.first_published_year;
    if (!yearA || !yearB) return Number(!yearA) - Number(!yearB) || titleOrder;
    return (yearA - yearB) * (sort === 'first_published_newest' ? -1 : 1) || titleOrder;
  }
  return titleOrder;
}
