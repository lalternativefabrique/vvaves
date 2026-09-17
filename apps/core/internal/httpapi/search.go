package httpapi

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/lalternative/packages/go/search"
)

const (
	maxContentResults   = 10
	defaultContentRunes = 4000
	contentDeadline     = 15 * time.Second
)

type searchRequest struct {
	Q          string   `json:"q"`
	Categories []string `json:"categories"`
	Limit      int      `json:"limit"`
	Language   string   `json:"language"`
	TimeRange  string   `json:"time_range"`
	Page       int      `json:"page"`
	DeadlineMS int64    `json:"deadline_ms"`
	// Content reads the first N results' pages into the response, so one
	// call gives an agent what it would otherwise fetch one by one.
	Content      int    `json:"content"`
	ContentRunes int    `json:"content_runes"`
	Format       string `json:"format"`
}

type searchResponse struct {
	Query   string   `json:"query"`
	Results []result `json:"results"`
	Partial bool     `json:"partial"`
}

// result restates search.Result in the snake_case this API speaks. The
// library's own struct carries no JSON tags, so serialising it directly would
// publish Go field names as the wire contract.
type result struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description string  `json:"description,omitempty"`
	Thumbnail   string  `json:"thumbnail,omitempty"`
	Source      string  `json:"source,omitempty"`
	Author      string  `json:"author,omitempty"`
	Duration    string  `json:"duration,omitempty"`
	PublishedAt string  `json:"published_at,omitempty"`
	SourceType  string  `json:"source_type,omitempty"`
	Score       float64 `json:"score"`
	Favicon     string  `json:"favicon,omitempty"`

	OpenGraph *openGraph `json:"open_graph,omitempty"`

	Text         string `json:"text,omitempty"`
	Markdown     string `json:"markdown,omitempty"`
	ContentError string `json:"content_error,omitempty"`
}

type openGraph struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	Type        string `json:"type,omitempty"`
}

func toResults(in []search.Result) []result {
	out := make([]result, 0, len(in))
	for _, r := range in {
		converted := result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
			Thumbnail:   r.Thumbnail,
			Source:      r.Source,
			Author:      r.Author,
			Duration:    r.Duration,
			PublishedAt: r.PublishedAt,
			SourceType:  r.SourceType,
			Score:       r.Score,
			Favicon:     r.Favicon,
		}
		if r.OpenGraph != nil {
			converted.OpenGraph = &openGraph{
				Title:       r.OpenGraph.Title,
				Description: r.OpenGraph.Description,
				Image:       r.OpenGraph.Image,
				SiteName:    r.OpenGraph.SiteName,
				Type:        r.OpenGraph.Type,
			}
		}
		out = append(out, converted)
	}
	return out
}

// handleSearch godoc
// @Summary  Ranked web results, optionally with the pages' text
// @Tags     web
// @Accept   json
// @Produce  json
// @Param    body  body      searchRequest  true  "query"
// @Success  200   {object}  searchResponse
// @Failure  400   {object}  errorResponse
// @Failure  502   {object}  errorResponse
// @Failure  503   {object}  errorResponse
// @Security ServiceKey
// @Security BearerAuth
// @Router   /search [post]
// @ID       search
func handleSearch(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(d.Providers) == 0 {
			writeError(w, http.StatusServiceUnavailable, "search is not configured")
			return
		}
		if err := d.guardService(r); err != nil {
			writeAuthError(w, err)
			return
		}

		var req searchRequest
		if err := decode(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.Q == "" {
			writeError(w, http.StatusBadRequest, "q is required")
			return
		}
		if req.Format != "" && req.Format != formatText && req.Format != formatMarkdown {
			writeError(w, http.StatusBadRequest, "format must be text or markdown")
			return
		}
		if req.Content > maxContentResults {
			req.Content = maxContentResults
		}

		categories, err := resolveCategories(d, req.Categories)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		q := search.Query{
			Text:      req.Q,
			Limit:     req.Limit,
			Language:  req.Language,
			TimeRange: req.TimeRange,
			Page:      req.Page,
		}

		var res *search.Response
		var searchErr error
		if len(categories) == 1 {
			q.Category = categories[0]
			res, searchErr = d.Providers[categories[0]].Search(r.Context(), q)
		} else {
			// Merge fuses the ranked lists by reciprocal rank instead of raw
			// score: SearXNG's web category peaks around 4.0 where its academic
			// engines cap at 1.0, so sorting a concatenation buries one of them.
			providers := make([]search.Provider, 0, len(categories))
			for _, c := range categories {
				providers = append(providers, categoryProvider{p: d.Providers[c], category: c})
			}
			res, searchErr = search.Merge(r.Context(), providers, q, deadline(d, req.DeadlineMS))
		}
		if searchErr != nil {
			writeError(w, http.StatusBadGateway, searchErr.Error())
			return
		}

		results := toResults(res.Results)
		if req.Content > 0 {
			attachContent(r.Context(), d, results, req)
		}
		writeJSON(w, http.StatusOK, searchResponse{
			Query:   res.Query,
			Results: results,
			Partial: res.Partial,
		})
	}
}

// attachContent reads the first req.Content results' pages in parallel,
// each through the /fetch path, and writes what came back onto the result.
// A page that fails or misses the deadline reports why on its own result
// rather than failing the search.
func attachContent(ctx context.Context, d Deps, results []result, req searchRequest) {
	ctx, cancel := context.WithTimeout(ctx, contentDeadline)
	defer cancel()

	maxRunes := req.ContentRunes
	if maxRunes <= 0 {
		maxRunes = defaultContentRunes
	}
	f := pageFetcher{d: d, maxRunes: maxRunes}

	n := req.Content
	if n > len(results) {
		n = len(results)
	}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(r *result) {
			defer wg.Done()
			page, err := f.Fetch(ctx, r.URL)
			if err != nil {
				r.ContentError = err.Error()
				return
			}
			if req.Format != formatMarkdown {
				r.Text = page.Text
			}
			if req.Format != formatText {
				r.Markdown = page.Markdown
			}
		}(&results[i])
	}
	wg.Wait()
}

func resolveCategories(d Deps, requested []string) ([]search.Category, error) {
	if len(requested) == 0 {
		if _, ok := d.Providers[search.CategoryGeneral]; ok {
			return []search.Category{search.CategoryGeneral}, nil
		}
		return nil, errUnknownCategory("")
	}
	seen := make(map[search.Category]bool, len(requested))
	out := make([]search.Category, 0, len(requested))
	for _, name := range requested {
		c := search.Category(name)
		if name == "academic" {
			c = search.CategoryAcademic
		}
		if _, ok := d.Providers[c]; !ok {
			return nil, errUnknownCategory(name)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

type unknownCategoryError string

func (e unknownCategoryError) Error() string {
	if e == "" {
		return "no category configured"
	}
	return "unknown category: " + string(e)
}

func errUnknownCategory(name string) error { return unknownCategoryError(name) }

func deadline(d Deps, requestedMS int64) time.Duration {
	if requestedMS <= 0 {
		return d.SearchDeadline
	}
	if asked := time.Duration(requestedMS) * time.Millisecond; asked < d.SearchDeadline {
		return asked
	}
	return d.SearchDeadline
}

// categoryProvider pins a category onto a provider, since Merge hands every
// provider the same Query and each one here answers for its own category.
type categoryProvider struct {
	p        search.Provider
	category search.Category
}

func (c categoryProvider) Search(ctx context.Context, q search.Query) (*search.Response, error) {
	q.Category = c.category
	return c.p.Search(ctx, q)
}
