package covers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"home-library/internal/store"
	"regexp"
	"strings"
)

var workKey = regexp.MustCompile(`^/works/OL[0-9]+W$`)

type editionRecord struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Publishers []string `json:"publishers"`
	Date       string   `json:"publish_date"`
	ISBN13     []string `json:"isbn_13"`
	ISBN10     []string `json:"isbn_10"`
	Format     string   `json:"physical_format"`
	Covers     []int64  `json:"covers"`
	Works      []struct {
		Key string `json:"key"`
	} `json:"works"`
	Languages []struct {
		Key string `json:"key"`
	} `json:"languages"`
}
type EditionOption struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Publisher string   `json:"publisher"`
	Date      string   `json:"publication_date"`
	ISBN      string   `json:"isbn"`
	Format    string   `json:"format"`
	Languages []string `json:"languages"`
	HasCover  bool     `json:"has_cover"`
}
type EditionPage struct {
	Editions []EditionOption `json:"editions"`
	Page     int             `json:"page"`
	HasMore  bool            `json:"has_more"`
}

func (s *Service) readEdition(ctx context.Context, address string) (editionRecord, error) {
	var e editionRecord
	data, err := s.get(ctx, address, 1<<20)
	if err != nil {
		return e, err
	}
	err = json.Unmarshal(data, &e)
	return e, err
}
func (s *Service) ownedEdition(ctx context.Context, id int64) (editionRecord, error) {
	b, e := s.Store.Book(ctx, id)
	if e != nil {
		return editionRecord{}, e
	}
	if editionID.MatchString(b.SourceID) {
		v, e := s.readEdition(ctx, "https://openlibrary.org/books/"+b.SourceID+".json")
		if e == nil {
			return v, nil
		}
		if !errors.Is(e, store.ErrNotFound) {
			return v, e
		}
	}
	if isbnID.MatchString(b.ISBN) {
		return s.readEdition(ctx, "https://openlibrary.org/isbn/"+b.ISBN+".json")
	}
	return editionRecord{}, errors.New("No linked Open Library edition found")
}
func linkedWork(e editionRecord) string {
	for _, w := range e.Works {
		if workKey.MatchString(w.Key) {
			return w.Key
		}
	}
	return ""
}
func (s *Service) Editions(ctx context.Context, id int64, page int) (EditionPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := EditionPage{Editions: []EditionOption{}, Page: page}
	if page < 1 || page > 100 {
		return result, errors.New("Choose an edition page between 1 and 100")
	}
	owned, e := s.ownedEdition(ctx, id)
	if e != nil {
		return result, e
	}
	work := linkedWork(owned)
	if work == "" {
		return result, errors.New("This edition has no linked work to browse")
	}
	data, e := s.get(ctx, fmt.Sprintf("https://openlibrary.org%s/editions.json?limit=10&offset=%d", work, (page-1)*10), 2<<20)
	if e != nil {
		return result, e
	}
	var list struct {
		Entries []editionRecord `json:"entries"`
		Size    int             `json:"size"`
	}
	if e = json.Unmarshal(data, &list); e != nil {
		return result, e
	}
	if len(list.Entries) > 10 {
		list.Entries = list.Entries[:10]
	}
	for _, v := range list.Entries {
		eid := strings.TrimPrefix(v.Key, "/books/")
		if !editionID.MatchString(eid) || v.Key == owned.Key {
			continue
		}
		option := EditionOption{ID: eid, Title: v.Title, Date: v.Date, Format: v.Format, Languages: []string{}}
		if len(v.Publishers) > 0 {
			option.Publisher = strings.Join(v.Publishers, ", ")
		}
		if len(v.ISBN13) > 0 {
			option.ISBN = v.ISBN13[0]
		} else if len(v.ISBN10) > 0 {
			option.ISBN = v.ISBN10[0]
		}
		for _, l := range v.Languages {
			option.Languages = append(option.Languages, strings.TrimPrefix(l.Key, "/languages/"))
		}
		for _, cid := range v.Covers {
			if cid > 0 {
				option.HasCover = true
				break
			}
		}
		result.Editions = append(result.Editions, option)
	}
	result.HasMore = page*10 < list.Size
	return result, nil
}

// PreviewEdition verifies shared-work membership, then caches only a small
// selection of artwork. It changes no catalogue metadata or selected cover.
func (s *Service) PreviewEdition(ctx context.Context, id int64, edition string) ([]store.CoverAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !editionID.MatchString(edition) {
		return nil, errors.New("Invalid edition ID")
	}
	owned, e := s.ownedEdition(ctx, id)
	if e != nil {
		return nil, e
	}
	work := linkedWork(owned)
	if work == "" {
		return nil, errors.New("No linked work")
	}
	candidate, e := s.readEdition(ctx, "https://openlibrary.org/books/"+edition+".json")
	if e != nil {
		return nil, e
	}
	belongs := false
	for _, w := range candidate.Works {
		if w.Key == work {
			belongs = true
		}
	}
	if !belongs || candidate.Key != "/books/"+edition {
		return nil, errors.New("This edition does not belong to the book's linked work")
	}
	existing, e := s.Store.Covers(ctx, id)
	if e != nil {
		return nil, e
	}
	known := map[int64]bool{}
	for _, a := range existing {
		if a.SourceCoverID != nil {
			known[*a.SourceCoverID] = true
		}
	}
	count := 0
	var lastError error
	for _, coverID := range candidate.Covers {
		if coverID <= 0 || known[coverID] {
			continue
		}
		if count >= 3 {
			break
		}
		count++
		data, e := s.get(ctx, fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg?default=false", coverID), MaxImageBytes)
		if e != nil {
			lastError = e
			continue
		}
		_, e = s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "other-edition", SourceCoverID: &coverID, SourceEditionID: edition}, data, false, false)
		if e != nil {
			lastError = e
		}
	}
	if lastError != nil && count > 0 {
		return nil, lastError
	}
	return s.Store.Covers(ctx, id)
}
