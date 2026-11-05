package api

import (
	"encoding/base64"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/arifurrahmanns/envry/internal/mimeparse"
)

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.ListMessages(r.Context(), listOptions(r))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetMessage(r.Context(), param(r, "id"))
	if err != nil {
		s.storeError(w, err, "message")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteMessage(r.Context(), param(r, "id")); err != nil {
		s.storeError(w, err, "message")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteAllMessages(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAllMessages(r.Context()); err != nil {
		s.internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRaw serves the exact bytes received. ?download=1 saves it as .eml.
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	id := param(r, "id")
	raw, err := s.store.Raw(r.Context(), id)
	if err != nil {
		s.storeError(w, err, "message")
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") != "" {
		h.Set("Content-Type", "message/rfc822")
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": id + ".eml"}))
	} else {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
	_, _ = w.Write(raw)
}

// handleAttachment serves an attachment as a download. It is never rendered
// in Envry's origin: HTML or SVG attachments cannot run script here.
func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	a, body, err := s.store.AttachmentContent(r.Context(), param(r, "id"), param(r, "aid"))
	if err != nil {
		s.storeError(w, err, "attachment")
		return
	}
	name := a.Filename
	if name == "" {
		name = "attachment-" + a.ID
		if exts, _ := mime.ExtensionsByType(a.ContentType); len(exts) > 0 {
			name += exts[0]
		}
	}
	h := w.Header()
	h.Set("Content-Type", a.ContentType)
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// handleHTML serves a message's HTML part for the sandboxed preview iframe.
//
// The markup is not rewritten beyond inlining cid: images, so the preview is
// faithful. Safety comes from the response policy instead: a CSP sandbox with
// no scripts, no forms and no same-origin access. Remote images, styles and
// fonts load only with ?remote=1.
func (s *Server) handleHTML(w http.ResponseWriter, r *http.Request) {
	raw, err := s.store.Raw(r.Context(), param(r, "id"))
	if err != nil {
		s.storeError(w, err, "message")
		return
	}
	p, err := mimeparse.Parse(raw)
	if err != nil || !p.HasHTML {
		writeError(w, http.StatusNotFound, "message has no HTML part")
		return
	}

	page := withBaseTarget(inlineCIDImages(p, raw))

	remote := ""
	if r.URL.Query().Get("remote") != "" {
		remote = " http: https:"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; "+
		"img-src data:"+remote+"; "+
		"style-src 'unsafe-inline'"+remote+"; "+
		"font-src data:"+remote+"; "+
		"media-src data:"+remote+"; "+
		"form-action 'none'; "+
		"sandbox allow-popups allow-popups-to-escape-sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

// maxInlineImageBytes caps how much cid: content is inlined as data: URIs.
const maxInlineImageBytes = 15 << 20

// inlineCIDImages replaces cid: references with data: URIs so embedded
// images render without the iframe needing access to Envry's origin.
func inlineCIDImages(p *mimeparse.Message, raw []byte) string {
	var pairs []string
	budget := maxInlineImageBytes
	for _, a := range p.Attachments {
		if a.ContentID == "" || a.Size > budget {
			continue
		}
		mediaType, body, err := mimeparse.ExtractPart(raw, a.PartPath)
		if err != nil {
			continue
		}
		budget -= len(body)
		uri := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(body)
		pairs = append(pairs, "cid:"+a.ContentID, uri, "CID:"+a.ContentID, uri)
	}
	if len(pairs) == 0 {
		return p.HTML
	}
	return strings.NewReplacer(pairs...).Replace(p.HTML)
}

var headTag = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)

// withBaseTarget makes links open in a new tab instead of inside the frame.
func withBaseTarget(html string) string {
	const base = `<base target="_blank">`
	if loc := headTag.FindStringIndex(html); loc != nil {
		return html[:loc[1]] + base + html[loc[1]:]
	}
	return base + html
}
