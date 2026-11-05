package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

const (
	wsWriteTimeout = 5 * time.Second
	wsPingInterval = 25 * time.Second
)

// HandleWS streams bus events to the browser as JSON {type, data} frames.
// Only same-origin pages may connect (the library's default origin check),
// so other websites cannot read captured mail through it.
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept has already written the HTTP error
	}
	defer c.CloseNow()

	ch, cancel := s.bus.Subscribe(256)
	defer cancel()

	// The client never sends anything; CloseRead handles control frames and
	// cancels ctx when the connection goes away.
	ctx := c.CloseRead(r.Context())
	ping := time.NewTicker(wsPingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closing:
			c.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(ev)
			if err != nil {
				s.log.Error("encode event", "err", err)
				continue
			}
			wctx, done := context.WithTimeout(ctx, wsWriteTimeout)
			err = c.Write(wctx, websocket.MessageText, b)
			done()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, done := context.WithTimeout(ctx, wsWriteTimeout)
			err := c.Ping(pctx)
			done()
			if err != nil {
				return
			}
		}
	}
}
