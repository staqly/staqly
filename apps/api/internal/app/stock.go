package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stock photos for the design editor, from Pixabay on the operator's key (PIXABAY_API_KEY). Without a key the
// editor hides the tab. The key stays on the server: the browser asks here and shows the thumbnails straight from
// Pixabay's CDN. Pixabay allows no permanent hotlinking, so a photo put on a design is first copied into the
// workspace's media library (UseStockPhoto) and the design uses that file; and it asks for answers to be cached
// for a day (pixabayCache).

// StockConfig: PIXABAY_API_KEY turns stock photos on; PixabayBaseURL is for tests.
type StockConfig struct {
	PixabayKey     string
	PixabayBaseURL string
}

type StockPhoto struct {
	ID        string `json:"id"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Alt       string `json:"alt"`
	Thumb     string `json:"thumb"` // at most 640 px
	URL       string `json:"url"`   // at most 1280 px; temporary, UseStockPhoto copies it into the library
	Author    string `json:"author"`
	AuthorURL string `json:"author_url"`
	PageURL   string `json:"page_url"`
}

type StockPage struct {
	Configured bool         `json:"configured"`
	Data       []StockPhoto `json:"data"`
}

var stockHTTP = &http.Client{Timeout: 30 * time.Second}

const (
	stockPerPage   = 30
	maxStockBytes  = 25 << 20
	pixabayCacheAt = 24 * time.Hour
	// ponytail: the whole cache goes when it is full; an LRU if searches outgrow it.
	pixabayCacheMax = 1000
)

type pixabayHit struct {
	ID            int64  `json:"id"`
	PageURL       string `json:"pageURL"`
	Tags          string `json:"tags"`
	WebformatURL  string `json:"webformatURL"`
	LargeImageURL string `json:"largeImageURL"`
	ImageWidth    int    `json:"imageWidth"`
	ImageHeight   int    `json:"imageHeight"`
	UserID        int64  `json:"user_id"`
	User          string `json:"user"`
}

var pixabayCache = struct {
	sync.Mutex
	m map[string]pixabayCached
}{m: map[string]pixabayCached{}}

type pixabayCached struct {
	at   time.Time
	hits []pixabayHit
}

// pixabay asks the API (or its cached answer from the last day) for the hits of q.
func (s *Service) pixabay(ctx context.Context, q url.Values) ([]pixabayHit, error) {
	q.Set("image_type", "photo")
	q.Set("safesearch", "true")
	base := strings.TrimRight(s.Cfg.Stock.PixabayBaseURL, "/")
	if base == "" {
		base = "https://pixabay.com/api"
	}
	ck := base + "?" + q.Encode()
	now := s.Now()
	pixabayCache.Lock()
	c, ok := pixabayCache.m[ck]
	pixabayCache.Unlock()
	if ok && now.Sub(c.at) < pixabayCacheAt {
		return c.hits, nil
	}
	q.Set("key", s.Cfg.Stock.PixabayKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	res, err := stockHTTP.Do(req)
	if err != nil {
		return nil, &Error{Code: "bad_gateway", Status: 502, Message: "Pixabay did not answer"}
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 429:
		return nil, &Error{Code: "rate_limited", Status: 429, Message: "Pixabay's limit for this server is used up; try again in a minute"}
	case res.StatusCode >= 300:
		return nil, &Error{Code: "bad_gateway", Status: 502, Message: fmt.Sprintf("Pixabay answered %d", res.StatusCode)}
	}
	var body struct {
		Hits []pixabayHit `json:"hits"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body); err != nil {
		return nil, &Error{Code: "bad_gateway", Status: 502, Message: "Pixabay answered something unexpected"}
	}
	pixabayCache.Lock()
	if len(pixabayCache.m) >= pixabayCacheMax {
		clear(pixabayCache.m)
	}
	pixabayCache.m[ck] = pixabayCached{at: now, hits: body.Hits}
	pixabayCache.Unlock()
	return body.Hits, nil
}

// StockPhotos searches Pixabay's photos (an empty query: its editors' choice), 30 a page.
func (s *Service) StockPhotos(ctx context.Context, p Principal, query string, page int) (StockPage, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return StockPage{}, err
	}
	if s.Cfg.Stock.PixabayKey == "" {
		return StockPage{Data: []StockPhoto{}}, nil
	}
	page = max(1, min(page, 16)) // Pixabay returns at most 500 hits a query
	q := url.Values{"page": {fmt.Sprint(page)}, "per_page": {fmt.Sprint(stockPerPage)}}
	if query = strings.TrimSpace(query); query != "" {
		if len(query) > 100 {
			return StockPage{}, invalid("query", "must be at most 100 characters")
		}
		q.Set("q", query)
	} else {
		q.Set("editors_choice", "true")
	}
	hits, err := s.pixabay(ctx, q)
	if err != nil {
		return StockPage{}, err
	}
	out := StockPage{Configured: true, Data: make([]StockPhoto, 0, len(hits))}
	for _, h := range hits {
		out.Data = append(out.Data, StockPhoto{ID: fmt.Sprint(h.ID), Width: h.ImageWidth, Height: h.ImageHeight, Alt: h.Tags,
			Thumb: h.WebformatURL, URL: h.LargeImageURL, Author: h.User,
			AuthorURL: fmt.Sprintf("https://pixabay.com/users/%s-%d/", url.PathEscape(h.User), h.UserID), PageURL: h.PageURL})
	}
	return out, nil
}

// UseStockPhoto copies a Pixabay photo (at most 1280 px) into the workspace's media library, for a design to use.
func (s *Service) UseStockPhoto(ctx context.Context, p Principal, id string) (Media, error) {
	if err := p.require(ScopeMediaWrite); err != nil {
		return Media{}, err
	}
	if s.Cfg.Stock.PixabayKey == "" {
		return Media{}, &Error{Code: "not_available", Status: 409, Message: "stock photos are off on this server"}
	}
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
		return Media{}, notFound("photo")
	}
	hits, err := s.pixabay(ctx, url.Values{"id": {id}})
	if err != nil {
		return Media{}, err
	}
	if len(hits) == 0 {
		return Media{}, notFound("photo")
	}
	u, err := url.Parse(hits[0].LargeImageURL)
	if err != nil || !s.pixabayHost(u) {
		return Media{}, &Error{Code: "bad_gateway", Status: 502, Message: "Pixabay answered something unexpected"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Media{}, err
	}
	res, err := stockHTTP.Do(req)
	if err != nil {
		return Media{}, &Error{Code: "bad_gateway", Status: 502, Message: "Pixabay did not send the photo"}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Media{}, &Error{Code: "bad_gateway", Status: 502, Message: fmt.Sprintf("Pixabay answered %d for the photo", res.StatusCode)}
	}
	return s.UploadMedia(ctx, p, "pixabay-"+id+".jpg", io.LimitReader(res.Body, maxStockBytes))
}

// pixabayHost: a photo is fetched from Pixabay only (or the test server standing in for it).
func (s *Service) pixabayHost(u *url.URL) bool {
	if b, err := url.Parse(s.Cfg.Stock.PixabayBaseURL); err == nil && b.Host != "" && u.Host == b.Host {
		return true
	}
	return u.Scheme == "https" && (u.Host == "pixabay.com" || strings.HasSuffix(u.Host, ".pixabay.com"))
}
