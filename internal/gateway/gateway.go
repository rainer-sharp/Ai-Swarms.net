// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Rainer Sestig und Mitwirkende.

// Package gateway stellt die lokale OpenAI-kompatible API, die Status-API und
// eine einfache Web-Oberfläche bereit. Es lauscht ausschließlich auf localhost.
package gateway

import (
	"cmp"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rainer-sharp/Ai-Swarms.net/internal/audio"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/orchestrator"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/profile"
	"github.com/rainer-sharp/Ai-Swarms.net/internal/protocol"
)

type Backend interface {
	Ask(ctx context.Context, req orchestrator.Request) (*orchestrator.Answer, error)
	StatusJSON() any
	// Pause, Resume und Stop wirken sofort; ein Fehler heißt, dass der
	// Zustand nicht gespeichert wurde und keinen Neustart übersteht.
	Pause() error
	Resume() error
	Stop() error
	Connect(ctx context.Context, addr string) error
	Ping(ctx context.Context, target, network string) (any, error)
	// Feedback und ProfileJSON: Kompetenzprofil (spec/cooperation.md § 2, § 8).
	Feedback(fb profile.Feedback) error
	ProfileJSON() any
}

//go:embed index.html
var indexHTML []byte

const modelPrefix = "swarmainet"

func Handler(b Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		var data []map[string]any
		for _, id := range []string{modelPrefix, modelPrefix + "/single", modelPrefix + "/parallel", modelPrefix + "/draft_verify_synthesize", modelPrefix + "/parallel_verify", modelPrefix + "/cascade", modelPrefix + "/decompose"} {
			data = append(data, map[string]any{"id": id, "object": "model", "owned_by": "swarmainet"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
	})
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { chat(b, w, r) })
	mux.HandleFunc("POST /v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) { transcribe(b, w, r) })
	if jb, ok := b.(JobBackend); ok {
		handleJobs(mux, jb)
	}
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, b.StatusJSON()) })
	control := func(path string, do func() error, done string) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, _ *http.Request) {
			if err := do(); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Errorf("%s, aber nicht gespeichert; gilt nur bis zum nächsten neustart: %w", done, err))
				return
			}
			writeJSON(w, 200, map[string]string{"ok": done})
		})
	}
	control("/api/pause", b.Pause, "pausiert")
	control("/api/resume", b.Resume, "fortgesetzt")
	control("/api/stop", b.Stop, "sofort-stopp")
	mux.HandleFunc("POST /api/connect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Addr string `json:"addr"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := b.Connect(ctx, req.Addr); err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "verbunden"})
	})
	mux.HandleFunc("POST /api/ping", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Peer    string `json:"peer"`
			Network string `json:"network"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		rep, err := b.Ping(ctx, req.Peer, req.Network)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, 200, rep)
	})
	mux.HandleFunc("POST /v1/feedback", func(w http.ResponseWriter, r *http.Request) {
		var fb profile.Feedback
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&fb); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		switch err := b.Feedback(fb); {
		case errors.Is(err, profile.ErrUnknownTask):
			writeErr(w, http.StatusNotFound, err)
		case errors.Is(err, profile.ErrDuplicate):
			writeErr(w, http.StatusConflict, err)
		case err != nil:
			writeErr(w, http.StatusBadRequest, err)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mux.HandleFunc("GET /api/profile", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, b.ProfileJSON()) })
	return localOnly(mux)
}

// localOnly weist Anfragen ab, die nicht von localhost kommen oder einen
// fremden Host-Header tragen (Schutz gegen DNS-Rebinding aus dem Browser).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "nur localhost", http.StatusForbidden)
			return
		}
		h := r.Host
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
		if h != "localhost" && h != "127.0.0.1" && h != "::1" && h != "[::1]" {
			http.Error(w, "ungültiger host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type chatRequest struct {
	Model      string          `json:"model"`
	Messages   []chatMessage   `json:"messages"`
	Stream     bool            `json:"stream"`
	MaxTokens  int             `json:"max_tokens"`
	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
}

// chatMessage ist eine Nachricht im OpenAI-Format. content ist Text, eine
// Liste von Teilen ([{"type":"text","text":…}]) oder null (Werkzeug-Aufruf).
type chatMessage struct {
	Role       string              `json:"role"`
	Content    json.RawMessage     `json:"content"`
	ToolCalls  []protocol.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
}

// errParts: Außer Text und Bildern verarbeitet der Schwarm keine Teile.
var errParts = errors.New("nur text und bilder (image_url) werden unterstützt")

// maxChatBody: Text und Bilder (Base64) einer Chat-Anfrage.
const maxChatBody = 8 << 20

// content liefert den Inhalt als Text und die eingebetteten Bilder; Teile vom
// Typ "text" werden verbunden.
func (m chatMessage) content() (string, []protocol.Image, error) {
	c := strings.TrimSpace(string(m.Content))
	if c == "" || c == "null" {
		return "", nil, nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s, nil, nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return "", nil, fmt.Errorf("content: erwartet text oder eine liste von teilen")
	}
	var texts []string
	var imgs []protocol.Image
	for _, p := range parts {
		switch p.Type {
		case "text":
			texts = append(texts, p.Text)
		case "image_url":
			img, err := dataImage(p.ImageURL.URL)
			if err != nil {
				return "", nil, err
			}
			imgs = append(imgs, img)
		default:
			return "", nil, errParts
		}
	}
	return strings.Join(texts, "\n"), imgs, nil
}

// dataImage liest ein eingebettetes Bild (data:image/png;base64,…). Links
// lädt das Gateway nicht nach: Es würde sonst für Fremde Adressen abrufen.
func dataImage(url string) (protocol.Image, error) {
	rest, ok := strings.CutPrefix(url, "data:")
	meta, data, ok2 := strings.Cut(rest, ",")
	mime, isB64 := strings.CutSuffix(meta, ";base64")
	if !ok || !ok2 || !isB64 {
		return protocol.Image{}, errors.New("bilder nur eingebettet als data-url (data:image/png;base64,…), keine links")
	}
	if !slices.Contains(protocol.ImageMIME, mime) {
		return protocol.Image{}, fmt.Errorf("bildformat %q nicht unterstützt (png, jpeg, webp, gif)", mime)
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(b) == 0 {
		return protocol.Image{}, errors.New("bild: base64 ungültig oder leer")
	}
	return protocol.Image{MIME: mime, Data: b}, nil
}

// parseChat liest eine Anfrage im Format der Chat-API. ok=false: Die
// Fehlerantwort ist schon geschrieben.
func parseChat(w http.ResponseWriter, r *http.Request) (req chatRequest, oreq orchestrator.Request, ok bool) {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBody)).Decode(&req); err != nil {
		if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("anfrage größer als %d bytes", maxChatBody))
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("messages fehlt"))
		return
	}
	msgs := make([]protocol.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		text, imgs, err := m.content()
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		msgs = append(msgs, protocol.ChatMessage{Role: m.Role, Content: text, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Name: m.Name, Images: imgs})
	}
	if n := protocol.ImageBytes(msgs); n > protocol.MaxImageBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("bilder zusammen %d bytes, erlaubt sind %d; "+
			"kleinere oder weniger bilder senden (auch frühere bilder im verlauf zählen)", n, protocol.MaxImageBytes))
		return
	}
	pattern := ""
	if rest, ok := strings.CutPrefix(req.Model, modelPrefix+"/"); ok {
		pattern = rest
	}
	oreq = orchestrator.Request{
		Messages:   msgs,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Pattern:    orchestrator.Pattern(pattern),
		Privacy:    protocol.PrivacyClass(r.Header.Get("X-SwarmAiNet-Privacy")),
		Skill:      r.Header.Get("X-SwarmAiNet-Skill"),
		Agents:     splitList(r.Header.Get("X-SwarmAiNet-Agents")),
		Trace:      r.Header.Get("X-SwarmAiNet-Trace") == "1",
	}
	oreq.Limits.MaxTokens = req.MaxTokens
	return req, oreq, true
}

func chat(b Backend, w http.ResponseWriter, r *http.Request) {
	req, oreq, ok := parseChat(w, r)
	if !ok {
		return
	}
	var sw *sseWriter
	if req.Stream {
		sw = newSSEWriter(w, req.Model)
		defer sw.stop()
		oreq.Stream = sw.event
	}
	ans, err := b.Ask(r.Context(), oreq)
	err = withHint(err)
	if sw != nil && sw.begun() {
		sw.finish(ans, err)
		return
	}
	if errors.Is(err, orchestrator.ErrToolsNotAllowed) || errors.Is(err, orchestrator.ErrPrivacy) {
		writeErr(w, http.StatusBadRequest, err) // Ursache liegt in der Anfrage
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if sw != nil {
		sw.finish(ans, nil)
		return
	}
	writeJSON(w, http.StatusOK, completion(ans, req.Model, time.Now()))
}

// completion: Antwort im Format der Chat-API.
func completion(ans *orchestrator.Answer, model string, created time.Time) map[string]any {
	finish := "stop"
	if len(ans.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	return map[string]any{
		"id": "chatcmpl-" + ans.TaskID, "object": "chat.completion", "created": created.Unix(), "model": model,
		"choices": []map[string]any{{
			"index": 0, "finish_reason": finish,
			"message": assistantMessage(ans),
		}},
		"swarmainet": ans,
	}
}

// keepAliveEvery: So oft geht während einer Pause im Stream ein SSE-Kommentar,
// damit Clients und Proxys die Verbindung nicht für tot halten.
const keepAliveEvery = 15 * time.Second

// sseWriter schreibt eine Antwort als Server-Sent Events im Format der
// OpenAI-Chat-API. Der Kopf geht erst mit dem ersten Zwischenstand hinaus;
// scheitert die Anfrage vorher, bleibt es bei einem normalen HTTP-Fehler.
// Die Denkphase steht in delta.reasoning_content, die Antwort in delta.content.
type sseWriter struct {
	w       http.ResponseWriter
	model   string
	id      string
	created int64

	mu      sync.Mutex
	started bool
	closed  bool            // Anfrage beendet: nichts mehr schreiben
	cur     strings.Builder // seit dem letzten Neustart gestreamter Antworttext
	last    time.Time
	quit    chan struct{}
}

func newSSEWriter(w http.ResponseWriter, model string) *sseWriter {
	var b [12]byte
	_, _ = rand.Read(b[:])
	s := &sseWriter{w: w, model: model, id: "chatcmpl-" + hex.EncodeToString(b[:]), created: time.Now().Unix(), quit: make(chan struct{})}
	go s.keepAlive()
	return s
}

func (s *sseWriter) keepAlive() {
	tk := time.NewTicker(keepAliveEvery / 3)
	defer tk.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-tk.C:
			s.mu.Lock()
			if s.started && !s.closed && time.Since(s.last) >= keepAliveEvery {
				_, _ = io.WriteString(s.w, ": keep-alive\n\n")
				s.flushLocked()
			}
			s.mu.Unlock()
		}
	}
}

func (s *sseWriter) stop() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	close(s.quit)
}

func (s *sseWriter) begun() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// startLocked schickt Kopf und Rolle.
func (s *sseWriter) startLocked() {
	if s.started {
		return
	}
	s.started = true
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
	s.chunkLocked(map[string]any{"role": "assistant", "content": ""}, nil, nil)
}

func (s *sseWriter) chunkLocked(delta map[string]any, finish any, extra *orchestrator.Answer) {
	m := map[string]any{
		"id": s.id, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}},
	}
	if extra != nil {
		m["swarmainet"] = extra
	}
	b, _ := json.Marshal(m)
	fmt.Fprintf(s.w, "data: %s\n\n", b)
	s.flushLocked()
}

func (s *sseWriter) flushLocked() {
	s.last = time.Now()
	if f, ok := s.w.(http.Flusher); ok {
		f.Flush()
	}
}

// restartNote trennt eine verworfene Fassung von der neuen.
const restartNote = "\n\n---\n*Diese Fassung wurde verworfen; es folgt eine neue.*\n\n"

// event nimmt einen Zwischenstand des Orchestrators an.
func (s *sseWriter) event(e orchestrator.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startLocked()
	switch e.Kind {
	case orchestrator.EventReasoning:
		s.chunkLocked(map[string]any{"reasoning_content": e.Text}, nil, nil)
	case orchestrator.EventContent:
		s.cur.WriteString(e.Text)
		s.chunkLocked(map[string]any{"content": e.Text}, nil, nil)
	case orchestrator.EventRestart:
		if s.cur.Len() > 0 {
			s.cur.Reset()
			s.chunkLocked(map[string]any{"content": restartNote}, nil, nil)
		}
	}
}

// finish schickt, was von der geprüften Antwort noch fehlt, dann das Ende mit
// den Metadaten. Ist der gestreamte Text kein Anfang der Antwort, folgt die
// ganze Antwort nach einem Trenner. Ein Fehler nach Beginn des Streams
// erscheint als Text, weil der Statuscode schon gesendet ist.
func (s *sseWriter) finish(ans *orchestrator.Answer, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startLocked()
	if err != nil {
		s.chunkLocked(map[string]any{"content": "\n\n**Fehler:** " + err.Error()}, nil, nil)
		s.chunkLocked(map[string]any{}, "stop", ans)
		_, _ = io.WriteString(s.w, "data: [DONE]\n\n")
		s.flushLocked()
		return
	}
	cur := s.cur.String()
	delta := map[string]any{}
	switch {
	case strings.HasPrefix(ans.Content, cur):
		if rest := ans.Content[len(cur):]; rest != "" {
			delta["content"] = rest
		}
	default:
		delta["content"] = restartNote + ans.Content
	}
	finish := "stop"
	if len(ans.ToolCalls) > 0 {
		finish = "tool_calls"
		calls := make([]map[string]any, len(ans.ToolCalls))
		for i, c := range ans.ToolCalls {
			calls[i] = map[string]any{"index": i, "id": c.ID, "type": c.Type, "function": c.Function}
		}
		delta["tool_calls"] = calls
	}
	if len(delta) > 0 {
		s.chunkLocked(delta, nil, nil)
	}
	s.chunkLocked(map[string]any{}, finish, ans)
	_, _ = io.WriteString(s.w, "data: [DONE]\n\n")
	s.flushLocked()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// splitList zerlegt eine kommagetrennte Header-Liste.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// withHint ergänzt Fehler, die der Nutzer selbst beheben kann, um einen Rat.
func withHint(err error) error {
	if errors.Is(err, protocol.ErrTokenLimit) {
		return fmt.Errorf("%w; das modell hat die token-grenze mit nachdenken verbraucht: max_tokens der anfrage oder tasks.max_tokens erhöhen", err)
	}
	return err
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"message": err.Error()}})
}

// assistantMessage: Antwort im OpenAI-Format; bei Werkzeug-Aufrufen ohne Text
// ist content null.
func assistantMessage(ans *orchestrator.Answer) map[string]any {
	m := map[string]any{"role": "assistant", "content": ans.Content}
	if len(ans.ToolCalls) > 0 {
		m["tool_calls"] = ans.ToolCalls
		if ans.Content == "" {
			m["content"] = nil
		}
	}
	return m
}

// maxUpload: Größte Aufnahme, die das Gateway annimmt. Über
// protocol.MaxAudioBytes wird sie in überlappende Stücke geteilt (ffmpeg).
const maxUpload = 512 << 20

// parallelChunks: So viele Stücke einer langen Aufnahme laufen gleichzeitig.
const parallelChunks = 4

// transcribe nimmt eine Aufnahme im Format der OpenAI-Transkriptions-API an
// (multipart/form-data: file, optional language, prompt, response_format) und
// lässt sie von Agenten mit audio.transcribe abschreiben. Das Feld model wird
// ignoriert; der Schwarm wählt das Modell. Lange Aufnahmen teilt das Gateway
// in überlappende Stücke, schickt sie parallel und fügt die Abschriften
// zusammen.
func transcribe(b Backend, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("aufnahme größer als %d MiB", maxUpload>>20))
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defer r.MultipartForm.RemoveAll()
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("feld file mit der aufnahme fehlt"))
		return
	}
	defer f.Close()
	format := cmp.Or(r.FormValue("response_format"), "json")
	if format != "json" && format != "text" && format != "verbose_json" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("response_format %q nicht unterstützt (json, text)", format))
		return
	}
	base := protocol.Audio{MIME: hdr.Header.Get("Content-Type"), Filename: hdr.Filename,
		Language: r.FormValue("language"), Prompt: r.FormValue("prompt")}
	privacy := protocol.PrivacyClass(r.Header.Get("X-SwarmAiNet-Privacy"))

	var text string
	var info any
	if hdr.Size <= protocol.MaxAudioBytes {
		data, err := io.ReadAll(f)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		a := base
		a.Data = data
		ans, err := b.Ask(r.Context(), orchestrator.Request{Audio: &a, Privacy: privacy})
		if err != nil {
			writeAskErr(w, err)
			return
		}
		text, info = ans.Content, ans
	} else {
		text, info, err = transcribeLong(r.Context(), b, f, base, privacy)
		switch {
		case errors.Is(err, audio.ErrNoFFmpeg):
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("aufnahme größer als %d bytes: %w", protocol.MaxAudioBytes, err))
			return
		case errors.Is(err, audio.ErrUnreadable):
			writeErr(w, http.StatusBadRequest, err)
			return
		case err != nil:
			writeAskErr(w, err)
			return
		}
	}
	if format == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, text)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text, "swarmainet": info})
}

func writeAskErr(w http.ResponseWriter, err error) {
	if errors.Is(err, orchestrator.ErrPrivacy) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeErr(w, http.StatusBadGateway, err)
}

// longTranscript beschreibt eine geteilte Aufnahme in der Antwort.
type longTranscript struct {
	DurationS float64                `json:"duration_s"`
	Chunks    int                    `json:"chunks"`
	Parts     []*orchestrator.Answer `json:"parts"`
}

// transcribeLong teilt die Aufnahme, lässt die Stücke parallel abschreiben
// und fügt die Abschriften in der Überlappung zusammen.
func transcribeLong(ctx context.Context, b Backend, f io.Reader, base protocol.Audio, privacy protocol.PrivacyClass) (string, any, error) {
	if !audio.Available() {
		return "", nil, audio.ErrNoFFmpeg
	}
	path, cleanup, err := spool(f)
	if err != nil {
		return "", nil, err
	}
	defer cleanup()
	chunks, total, err := audio.Split(ctx, path, audio.ChunkLength, audio.Overlap)
	if err != nil {
		return "", nil, err
	}
	answers, err := askChunks(ctx, b, chunks, base, privacy)
	if err != nil {
		return "", nil, err
	}
	texts, share := make([]string, len(chunks)), make([]float64, 0, len(chunks))
	for i, c := range chunks {
		texts[i] = answers[i].Content
		if i > 0 {
			share = append(share, audio.Overlap.Seconds()/c.Len.Seconds())
		}
	}
	return audio.Merge(texts, share), longTranscript{DurationS: total.Seconds(), Chunks: len(chunks), Parts: answers}, nil
}

// busyRequeues: So oft kommt ein Stück zurück in die Warteschlange, wenn alle
// Anbieter auch nach der Wartezeit des Orchestrators ausgelastet sind.
const busyRequeues = 5

// askChunks lässt die Stücke mit höchstens parallelChunks Aufträgen zugleich
// abschreiben. Ein fehlgeschlagenes Stück bricht die übrigen ab; gemeldet
// wird die Ursache, nicht der Folgeabbruch.
func askChunks(ctx context.Context, b Backend, chunks []audio.Chunk, base protocol.Audio, privacy protocol.PrivacyClass) ([]*orchestrator.Answer, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	n := len(chunks)
	answers, errs, requeued := make([]*orchestrator.Answer, n), make([]error, n), make([]int, n)
	jobs := make(chan int, n)
	for i := range n {
		jobs <- i
	}
	var done sync.WaitGroup
	done.Add(n)
	for range min(parallelChunks, n) {
		go func() {
			for i := range jobs {
				a := base
				a.Data, a.MIME = chunks[i].Data, "audio/mpeg"
				a.Filename = fmt.Sprintf("stueck-%02d.mp3", i)
				ans, err := b.Ask(ctx, orchestrator.Request{Audio: &a, Privacy: privacy})
				if errors.Is(err, orchestrator.ErrBusy) && ctx.Err() == nil && requeued[i] < busyRequeues {
					requeued[i]++
					jobs <- i // Platz ist da: jedes Stück steht höchstens einmal in der Schlange
					continue
				}
				answers[i], errs[i] = ans, err
				if err != nil {
					cancel() // ein fehlendes Stück macht die Abschrift unbrauchbar
				}
				done.Done()
			}
		}()
	}
	done.Wait()
	close(jobs)
	first := -1
	for i, err := range errs {
		if err != nil && (first < 0 || errors.Is(errs[first], context.Canceled) && !errors.Is(err, context.Canceled)) {
			first = i
		}
	}
	if first >= 0 {
		return nil, fmt.Errorf("stück %d (ab %s): %w", first, chunks[first].Start.Round(time.Second), errs[first])
	}
	return answers, nil
}

// spool legt die hochgeladene Aufnahme als Datei ab, damit ffmpeg sie lesen kann.
func spool(f io.Reader) (string, func(), error) {
	if of, ok := f.(*os.File); ok {
		return of.Name(), func() {}, nil
	}
	tmp, err := os.CreateTemp("", "swarmainet-audio-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { tmp.Close(); os.Remove(tmp.Name()) }
	if _, err := io.Copy(tmp, f); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp.Name(), cleanup, nil
}
