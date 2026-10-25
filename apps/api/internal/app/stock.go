package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Stock photos for the design editor, from Unsplash on the operator's key (UNSPLASH_ACCESS_KEY). Without a key
// the editor hides the tab. The key stays on the server: the browser asks here, and loads the pictures
// themselves straight from Unsplash's CDN (which allows any origin, so the canvas can export them).

// StockConfig: UNSPLASH_ACCESS_KEY turns stock photos on; UnsplashBaseURL is for tests.
type StockConfig struct {
	UnsplashKey     string
	UnsplashBaseURL string
}

type StockPhoto struct {
	ID     string `json:"id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Alt    string `json:"alt"`
	Thumb  string `json:"thumb"` // about 400 px wide
	URL    string `json:"url"`   // about 2000 px wide, for the canvas
	Author string `json:"author"`
	// AuthorURL and PageURL carry Unsplash's referral parameters, as its guidelines ask.
	AuthorURL string `json:"author_url"`
	PageURL   string `json:"page_url"`
}

type StockPage struct {
	Configured bool         `json:"configured"`
	Data       []StockPhoto `json:"data"`
}

var stockHTTP = &http.Client{Timeout: 15 * time.Second}

type unsplashPhoto struct {
	ID          string `json:"id"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Alt         string `json:"alt_description"`
	Description string `json:"description"`
	URLs        struct {
		Small string `json:"small"`
		Raw   string `json:"raw"`
	} `json:"urls"`
	Links struct {
		HTML string `json:"html"`
	} `json:"links"`
	User struct {
		Name  string `json:"name"`
		Links struct {
			HTML string `json:"html"`
		} `json:"links"`
	} `json:"user"`
}

func (s *Service) unsplash(ctx context.Context, path string, q url.Values, out any) error {
	base := strings.TrimRight(s.Cfg.Stock.UnsplashBaseURL, "/")
	if base == "" {
		base = "https://api.unsplash.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Client-ID "+s.Cfg.Stock.UnsplashKey)
	req.Header.Set("Accept-Version", "v1")
	res, err := stockHTTP.Do(req)
	if err != nil {
		return &Error{Code: "bad_gateway", Status: 502, Message: "Unsplash did not answer"}
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == 403 || res.StatusCode == 429:
		return &Error{Code: "rate_limited", Status: 429, Message: "Unsplash's hourly limit for this server is used up; try again later"}
	case res.StatusCode >= 300:
		return &Error{Code: "bad_gateway", Status: 502, Message: fmt.Sprintf("Unsplash answered %d", res.StatusCode)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func withRef(u string) string {
	if u == "" {
		return ""
	}
	return u + "?utm_source=staqly&utm_medium=referral"
}

// StockPhotos searches Unsplash (an empty query: its editorial feed), 30 a page.
func (s *Service) StockPhotos(ctx context.Context, p Principal, query string, page int) (StockPage, error) {
	if err := p.require(ScopeMediaRead); err != nil {
		return StockPage{}, err
	}
	if s.Cfg.Stock.UnsplashKey == "" {
		return StockPage{Data: []StockPhoto{}}, nil
	}
	page = max(1, min(page, 100))
	q := url.Values{"page": {fmt.Sprint(page)}, "per_page": {"30"}}
	var list []unsplashPhoto
	if query = strings.TrimSpace(query); query != "" {
		if len(query) > 100 {
			return StockPage{}, invalid("query", "must be at most 100 characters")
		}
		q.Set("query", query)
		var res struct {
			Results []unsplashPhoto `json:"results"`
		}
		if err := s.unsplash(ctx, "/search/photos", q, &res); err != nil {
			return StockPage{}, err
		}
		list = res.Results
	} else if err := s.unsplash(ctx, "/photos", q, &list); err != nil {
		return StockPage{}, err
	}
	out := StockPage{Configured: true, Data: make([]StockPhoto, 0, len(list))}
	for _, u := range list {
		alt := u.Alt
		if alt == "" {
			alt = u.Description
		}
		full := u.URLs.Raw
		if full != "" {
			sep := "?"
			if strings.Contains(full, "?") {
				sep = "&"
			}
			full += sep + "w=2000&fm=jpg&q=85"
		}
		out.Data = append(out.Data, StockPhoto{ID: u.ID, Width: u.Width, Height: u.Height, Alt: alt, Thumb: u.URLs.Small,
			URL: full, Author: u.User.Name, AuthorURL: withRef(u.User.Links.HTML), PageURL: withRef(u.Links.HTML)})
	}
	return out, nil
}

// UseStockPhoto tells Unsplash a photo was put into a design (its API guidelines require this "download" ping).
func (s *Service) UseStockPhoto(ctx context.Context, p Principal, id string) error {
	if err := p.require(ScopeMediaWrite); err != nil {
		return err
	}
	if s.Cfg.Stock.UnsplashKey == "" {
		return &Error{Code: "not_available", Status: 409, Message: "stock photos are off on this server"}
	}
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/?#%") {
		return notFound("photo")
	}
	return s.unsplash(ctx, "/photos/"+id+"/download", url.Values{}, nil)
}
