package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/initializer"
	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/lark"
	"github.com/youdisn/lark-ob/internal/maintenance"
	"github.com/youdisn/lark-ob/internal/media"
	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/profile"
	"github.com/youdisn/lark-ob/internal/store"
	"github.com/youdisn/lark-ob/internal/syncer"
)

//go:embed ui/dist/*
var uiFiles embed.FS

type Server struct {
	store       *store.Store
	lark        *lark.Client
	syncer      *syncer.Syncer
	knowledge   *knowledge.Service
	agent       *agent.Engine
	memories    *memory.Service
	initializer *initializer.Service
	profiles    *profile.Service
	impressions *impression.Service
	maintenance *maintenance.Service
	media       *media.Service
	stateMu     sync.Mutex
	oauthState  string
	clientsMu   sync.Mutex
	clients     map[chan struct{}]struct{}
}

func New(st *store.Store, lc *lark.Client, sy *syncer.Syncer, ks *knowledge.Service, ae *agent.Engine, memories *memory.Service, init *initializer.Service, profiles *profile.Service, impressions *impression.Service, maintenance *maintenance.Service, mediaService *media.Service) *Server {
	s := &Server{store: st, lark: lc, syncer: sy, knowledge: ks, agent: ae, memories: memories, initializer: init, profiles: profiles,
		impressions: impressions, maintenance: maintenance, media: mediaService, clients: map[chan struct{}]struct{}{}}
	sy.SetNotify(s.broadcast)
	if profiles != nil {
		profiles.SetNotify(s.broadcast)
	}
	if init != nil {
		init.SetNotify(s.broadcast)
	}
	if maintenance != nil {
		maintenance.SetNotify(s.broadcast)
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/chats", s.chats)
	mux.HandleFunc("GET /api/chats/{id}/messages", s.messages)
	mux.HandleFunc("GET /api/chats/{id}/history", s.history)
	mux.HandleFunc("POST /api/chats/{id}/history", s.loadHistory)
	mux.HandleFunc("POST /api/chats/{id}/refresh", s.refreshChat)
	mux.HandleFunc("POST /api/chats/{id}/viewed", s.markChatViewed)
	mux.HandleFunc("POST /api/chats/read-all", s.markAllChatsViewed)
	mux.HandleFunc("PATCH /api/chats/{id}/preferences", s.updateChatPreferences)
	mux.HandleFunc("GET /api/messages/{id}/images/{key}", s.messageImage)
	mux.HandleFunc("POST /api/sync", s.syncNow)
	s.registerKnowledgeRoutes(mux)
	s.registerAgentRoutes(mux)
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
	if s.profiles != nil {
		s.profiles.Enqueue(v)
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
func (s *Server) refreshChat(w http.ResponseWriter, r *http.Request) {
	result, err := s.syncer.SyncChat(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}
func (s *Server) markChatViewed(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MarkChatViewed(r.Context(), r.PathValue("id")); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, fmt.Errorf("会话不存在"), http.StatusNotFound)
			return
		}
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) markAllChatsViewed(w http.ResponseWriter, r *http.Request) {
	updated, err := s.store.MarkAllChatsViewed(r.Context())
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.broadcast()
	writeJSON(w, map[string]any{"ok": true, "updated": updated})
}
func (s *Server) updateChatPreferences(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Muted        *bool `json:"muted"`
		InMessageBox *bool `json:"inMessageBox"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, fmt.Errorf("无效的会话状态: %w", err), http.StatusBadRequest)
		return
	}
	if err := s.store.SetChatPreferences(r.Context(), r.PathValue("id"), request.Muted, request.InMessageBox); err != nil {
		switch {
		case err == sql.ErrNoRows:
			writeError(w, fmt.Errorf("会话不存在"), http.StatusNotFound)
		case strings.Contains(err.Error(), "只有群聊") || strings.Contains(err.Error(), "至少需要"):
			writeError(w, err, http.StatusBadRequest)
		default:
			writeError(w, err, http.StatusInternalServerError)
		}
		return
	}
	if request.InMessageBox != nil && *request.InMessageBox && s.memories != nil {
		if _, err := s.memories.ReplaceChat(r.Context(), r.PathValue("id"), nil); err != nil {
			writeError(w, fmt.Errorf("会话已移入消息盒子，但清理已有记忆失败: %w", err), http.StatusInternalServerError)
			return
		}
		if err := s.store.InvalidateDerivedProfilesForChat(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, fmt.Errorf("会话已移入消息盒子，但清理关联印象失败: %w", err), http.StatusInternalServerError)
			return
		}
	}
	chat, err := s.store.Chat(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.broadcast()
	writeJSON(w, chat)
}
func (s *Server) messageImage(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		writeError(w, fmt.Errorf("图片服务未启用"), http.StatusServiceUnavailable)
		return
	}
	path, contentType, err := s.media.Image(r.Context(), r.PathValue("id"), r.PathValue("key"))
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		writeError(w, err, http.StatusBadGateway)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", info.ModTime(), file)
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
