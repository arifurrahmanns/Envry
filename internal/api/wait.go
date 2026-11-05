package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/arifurrahmanns/envry/internal/events"
	"github.com/arifurrahmanns/envry/internal/store"
)

const (
	defaultWaitTimeout = 10 * time.Second
	maxWaitTimeout     = 5 * time.Minute
)

// handleWait long-polls until a message matching the filters exists, then
// returns it in full. It is meant for automated tests:
//
//	GET /api/v1/wait?to=user@test.local&subject=Reset&timeout=10s
//
// Filters: to and from (exact address), subject (case-insensitive substring)
// and since (RFC 3339; only messages received at or after it). A message
// that already arrived matches immediately, so tests do not race the
// sender. Responds 408 when the timeout passes first.
func (s *Server) handleWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	timeout, err := parseTimeout(q.Get("timeout"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "timeout must be a duration such as 10s or a number of seconds")
		return
	}
	filter := store.WaitFilter{To: q.Get("to"), From: q.Get("from"), Subject: q.Get("subject")}
	if v := q.Get("since"); v != "" {
		filter.Since, err = time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since must be an RFC 3339 timestamp")
			return
		}
	}

	// Subscribe before the first lookup so nothing slips in between.
	ch, cancel := s.bus.Subscribe(64)
	defer cancel()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		m, err := s.store.LatestMatching(r.Context(), filter)
		if err == nil {
			detail, err := s.store.GetMessage(r.Context(), m.ID)
			if err != nil {
				s.storeError(w, err, "message")
				return
			}
			writeJSON(w, http.StatusOK, detail)
			return
		}
		if !errors.Is(err, store.ErrNotFound) {
			s.internalError(w, err)
			return
		}

	wait:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-s.closing:
				writeError(w, http.StatusServiceUnavailable, "server is shutting down")
				return
			case <-timer.C:
				writeError(w, http.StatusRequestTimeout, "timed out waiting for a matching message")
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if ev.Type == events.MessageCreated {
					break wait
				}
			}
		}
	}
}

func parseTimeout(v string) (time.Duration, error) {
	if v == "" {
		return defaultWaitTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		secs, err2 := strconv.ParseFloat(v, 64)
		if err2 != nil {
			return 0, err
		}
		d = time.Duration(secs * float64(time.Second))
	}
	if d <= 0 {
		return 0, errors.New("timeout must be positive")
	}
	return min(d, maxWaitTimeout), nil
}
