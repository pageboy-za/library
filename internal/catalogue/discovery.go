// Package catalogue contains catalogue operations shared by browser and tool interfaces.
package catalogue

import (
	"context"
	"errors"
	"golang.org/x/text/unicode/norm"
	"home-library/internal/store"
	"sort"
	"strings"
	"unicode"
)

type DiscoveryResult struct {
	Book    store.Book `json:"book"`
	Reasons []string   `json:"reasons"`
	Score   int        `json:"score"`
}
type DiscoveryService struct{ Store *store.Store }

func normal(s string) string { return strings.ToLower(norm.NFKC.String(s)) }
func queryTerms(query string) []string {
	stop := map[string]bool{"a": true, "an": true, "the": true, "books": true, "book": true, "about": true, "on": true, "of": true, "for": true, "and": true, "or": true, "to": true, "that": true, "with": true, "me": true, "find": true, "related": true, "ideas": true, "idea": true}
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range strings.FieldsFunc(normal(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if !stop[term] && !seen[term] {
			terms = append(terms, term)
			seen[term] = true
		}
	}
	return terms
}

// Search explains literal keyword matches; it does not infer semantic similarity.
func (s DiscoveryService) Search(ctx context.Context, query string) ([]DiscoveryResult, error) {
	if len(query) > 500 {
		return nil, errors.New("Keep discovery searches under 500 characters")
	}
	terms := queryTerms(query)
	results := []DiscoveryResult{}
	if len(terms) == 0 {
		return results, nil
	}
	rows, err := s.Store.DB.QueryContext(ctx, `SELECT id FROM books WHERE deleted_at IS NULL AND EXISTS(SELECT 1 FROM copies WHERE book_id=books.id AND archived_at IS NULL) ORDER BY id`)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		book, err := s.Store.Book(ctx, id)
		if err != nil {
			return nil, err
		}
		d, err := s.Store.Discovery(ctx, id)
		if err != nil {
			return nil, err
		}
		fields := []struct {
			name, value string
			weight      int
		}{{"title", book.Title, 4}, {"author", strings.Join(book.Authors, " "), 3}, {"subject", strings.Join(d.Subjects, " "), 4}, {"your tag", strings.Join(d.Tags, " "), 5}, {"description", d.Description, 2}, {"your note", d.Notes, 2}}
		result := DiscoveryResult{Book: book, Reasons: []string{}}
		for _, field := range fields {
			found := []string{}
			for _, term := range terms {
				if strings.Contains(normal(field.value), term) {
					found = append(found, term)
					result.Score += field.weight
				}
			}
			if len(found) > 0 {
				result.Reasons = append(result.Reasons, "Matches "+field.name+": "+strings.Join(found, ", "))
			}
		}
		if result.Score > 0 {
			results = append(results, result)
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Book.Title != results[j].Book.Title {
			return results[i].Book.Title < results[j].Book.Title
		}
		return results[i].Book.ID < results[j].Book.ID
	})
	if len(results) > 50 {
		results = results[:50]
	}
	return results, nil
}
