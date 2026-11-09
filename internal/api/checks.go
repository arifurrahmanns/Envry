package api

import (
	"net/http"

	"github.com/arifurrahmanns/envry/internal/checks"
	"github.com/arifurrahmanns/envry/internal/extract"
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
	p, raw, ok := s.parsedMessage(w, r)
	if !ok {
		return checks.Report{}, false
	}
	return checks.Run(p, len(raw)), true
}

// handleExtracts returns the message's one-time codes and action links
// (verify, reset, sign-in, invitation), best match first.
func (s *Server) handleExtracts(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.parsedMessage(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, extract.Find(p))
}

// parsedMessage parses the stored message named by the {id} route
// parameter, writing the error response when it cannot.
func (s *Server) parsedMessage(w http.ResponseWriter, r *http.Request) (*mimeparse.Message, []byte, bool) {
	raw, err := s.store.Raw(r.Context(), param(r, "id"))
	if err != nil {
		s.storeError(w, err, "message")
		return nil, nil, false
	}
	p, err := mimeparse.Parse(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "message could not be parsed: "+err.Error())
		return nil, nil, false
	}
	return p, raw, true
}
