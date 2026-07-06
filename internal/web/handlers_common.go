package web

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// base builds the common template data shared by every page (nav state, base
// currency, and the list of years for the year switchers).
func (s *Server) base(ctx context.Context, title, active string) map[string]any {
	years, _ := s.store.AvailableYears(ctx)
	return map[string]any{
		"Title":        title,
		"Nav":          active,
		"BaseCurrency": s.cfg.BaseCurrency,
		"Years":        years,
		"CurrentYear":  time.Now().Year(),
		"AIProvider":   s.cfg.AIProvider,
	}
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/reports/year-overview?year="+strconv.Itoa(time.Now().Year()), http.StatusFound)
}

// intParam reads an integer query/form value with a default.
func intParam(r *http.Request, key string, def int) int {
	if v := r.FormValue(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func int64PathValue(r *http.Request, key string) (int64, error) {
	return strconv.ParseInt(r.PathValue(key), 10, 64)
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
