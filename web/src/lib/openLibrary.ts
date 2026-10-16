import type {Book} from './api';

export function openLibraryUrl(book:Partial<Pick<Book,'source_id'|'source_url'|'isbn'|'first_published_year_source_url'>>):string {
 return book.source_id&&/^OL\d+M$/.test(book.source_id)
 ? `https://openlibrary.org/books/${book.source_id}`
 : book.source_url&&/^https:\/\/openlibrary\.org\/(books\/OL\d+M|works\/OL\d+W)(\/[^?#]*)?$/.test(book.source_url)
 ? book.source_url
 : book.isbn ? `https://openlibrary.org/isbn/${encodeURIComponent(book.isbn)}`
 : book.first_published_year_source_url&&/^https:\/\/openlibrary\.org\/works\/OL\d+W$/.test(book.first_published_year_source_url)
 ? book.first_published_year_source_url : '';
}
