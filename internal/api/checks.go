package api

import (
	"net/http"

	"github.com/arifurrahmanns/envry/internal/checks"
	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

// handleChecks runs the static message checks. It makes no network requests.
func (s *Server) handleChecks(w http.ResponseWriter, r *http.Request) {
	report, ok := s.checkReport(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type linkResultsDTO struct {
	Results []checks.LinkResult `json:"results"`
	// Total is the number of links in the message; only the first
	// checks.MaxLinks are requested.
	Total int `json:"total"`
}

// handleCheckLinks requests the message's http(s) links. It is a POST
// because it reaches out to the network and only runs when the user asks.
func (s *Server) handleCheckLinks(w http.ResponseWriter, r *http.Request) {
	report, ok := s.checkReport(w, r)
	if !ok {
		return
	}
	urls := make([]string, len(report.Links))
	for i, l := range report.Links {
		urls[i] = l.URL
	}
	writeJSON(w, http.StatusOK, linkResultsDTO{
		Results: checks.CheckLinks(r.Context(), nil, urls),
		Total:   len(urls),
	})
}

func (s *Server) checkReport(w http.ResponseWriter, r *http.Request) (checks.Report, bool) {
	raw, err := s.store.Raw(r.Context(), param(r, "id"))
	if err != nil {
		s.storeError(w, err, "message")
		return checks.Report{}, false
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "message could not be parsed: "+err.Error())
		return checks.Report{}, false
	}
	return checks.Run(p, len(raw)), true
}
