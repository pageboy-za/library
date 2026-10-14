import {readFileSync} from 'node:fs';
import type {Book} from './api';
export type StaticBook = Pick<Book,'id'|'isbn'|'title'|'authors'|'publisher'|'publication_date'|'cover_url'|'revision'> & {description:string;subjects:string[]};
export function bookSnapshot():StaticBook[]{
 const path=process.env.LIBRARY_BOOK_SNAPSHOT;
 return path ? JSON.parse(readFileSync(path,'utf8')) : [];
}
