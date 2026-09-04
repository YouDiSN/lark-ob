package server

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/lark"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
	"github.com/youdisn/lark-ob/internal/syncer"
)

//go:embed ui/dist/*
var uiFiles embed.FS

type Server struct {
	store      *store.Store
	lark       *lark.Client
	syncer     *syncer.Syncer
	stateMu    sync.Mutex
	oauthState string
	clientsMu  sync.Mutex
	clients    map[chan struct{}]struct{}
}

func New(st *store.Store, lc *lark.Client, sy *syncer.Syncer) *Server {
	s := &Server{store: st, lark: lc, syncer: sy, clients: map[chan struct{}]struct{}{}}
	sy.SetNotify(s.broadcast)
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/chats", s.chats)
	mux.HandleFunc("GET /api/chats/{id}/messages", s.messages)
	mux.HandleFunc("GET /api/chats/{id}/history", s.history)
	mux.HandleFunc("POST /api/chats/{id}/history", s.loadHistory)
	mux.HandleFunc("POST /api/sync", s.syncNow)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /auth/lark", s.authStart)
	mux.HandleFunc("GET /auth/lark/callback", s.authCallback)
	dist, _ := fs.Sub(uiFiles, "ui/dist")
	files := http.FileServer(http.FS(dist))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/" {
			if _, err := fs.Stat(dist, strings.TrimPrefix(r.URL.Path, "/")); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
	return logging(mux)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.syncer.Status()) }
func (s *Server) chats(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.Chats(r.Context())
	if err != nil {
		writeError(w, err, 500)
		return
	}
	if v == nil {
		v = []model.Chat{}
	}
	writeJSON(w, v)
}
func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	v, err := s.store.Messages(r.Context(), r.PathValue("id"), 200)
	if err != nil {
		writeError(w, err, 500)
		return
	}
	if v == nil {
		v = []model.Message{}
	}
	writeJSON(w, v)
}
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.syncer.History(r.Context(), r.PathValue("id")))
}
func (s *Server) loadHistory(w http.ResponseWriter, r *http.Request) {
	count, state, err := s.syncer.LoadOlder(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err, 502)
		return
	}
	writeJSON(w, map[string]any{"loaded": count, "hasMore": state.HasMore})
}
func (s *Server) syncNow(w http.ResponseWriter, r *http.Request) {
	go s.syncer.Sync(context.Background())
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) authStart(w http.ResponseWriter, r *http.Request) {
	if !s.lark.Configured() {
		writeError(w, fmt.Errorf("请先配置 LARK_APP_ID 和 LARK_APP_SECRET"), 400)
		return
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)
	s.stateMu.Lock()
	s.oauthState = state
	s.stateMu.Unlock()
	http.Redirect(w, r, s.lark.AuthorizeURL(state), http.StatusFound)
}
func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	s.stateMu.Lock()
	valid := s.oauthState != "" && r.URL.Query().Get("state") == s.oauthState
	s.oauthState = ""
	s.stateMu.Unlock()
	if !valid {
		writeError(w, fmt.Errorf("OAuth state 校验失败"), 400)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, fmt.Errorf("Lark 未返回授权码"), 400)
		return
	}
	t, err := s.lark.ExchangeCode(r.Context(), code)
	if err != nil {
		writeError(w, err, 502)
		return
	}
	if err := s.syncer.SaveToken(r.Context(), t); err != nil {
		writeError(w, err, 500)
		return
	}
	go s.syncer.Sync(context.Background())
	http.Redirect(w, r, "/?connected=1", http.StatusFound)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, fmt.Errorf("stream unsupported"), 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan struct{}, 1)
	s.clientsMu.Lock()
	s.clients[ch] = struct{}{}
	s.clientsMu.Unlock()
	defer func() { s.clientsMu.Lock(); delete(s.clients, ch); s.clientsMu.Unlock() }()
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: update\ndata: {}\n\n")
			flusher.Flush()
		case <-time.After(25 * time.Second):
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
func (s *Server) broadcast() {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for c := range s.clients {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}
