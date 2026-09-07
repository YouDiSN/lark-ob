package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/youdisn/lark-ob/internal/knowledge"
)

func (s *Server) registerKnowledgeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/knowledge/sources", s.knowledgeSources)
	mux.HandleFunc("POST /api/knowledge/sources", s.importKnowledgeSource)
	mux.HandleFunc("POST /api/knowledge/sources/{id}/sync", s.syncKnowledgeSource)
	mux.HandleFunc("DELETE /api/knowledge/sources/{id}", s.deleteKnowledgeSource)
	mux.HandleFunc("GET /api/knowledge/search", s.searchKnowledge)
}

func (s *Server) knowledgeSources(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	sources, err := s.knowledge.List(r.Context())
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if sources == nil {
		sources = []knowledge.Source{}
	}
	writeJSON(w, sources)
}

func (s *Server) importKnowledgeSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()
	var request knowledge.ImportRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, fmt.Errorf("无效的导入请求: %w", err), http.StatusBadRequest)
		return
	}
	if request.ScopeType == "" {
		request.ScopeType = knowledge.ScopeGlobal
	}
	source, err := s.knowledge.Import(r.Context(), request)
	if err != nil {
		writeError(w, err, http.StatusUnprocessableEntity)
		return
	}
	s.broadcast()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, source)
}

func (s *Server) syncKnowledgeSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	id, err := parseKnowledgeSourceID(r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	source, err := s.knowledge.Sync(r.Context(), id)
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	s.broadcast()
	writeJSON(w, source)
}

func (s *Server) deleteKnowledgeSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	id, err := parseKnowledgeSourceID(r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if err := s.knowledge.Delete(r.Context(), id); err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	s.broadcast()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) searchKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.knowledge.Search(r.Context(), r.URL.Query().Get("q"), knowledge.SearchOptions{
		ScopeType: strings.TrimSpace(r.URL.Query().Get("scopeType")),
		ScopeID:   strings.TrimSpace(r.URL.Query().Get("scopeId")),
		Limit:     limit,
	})
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if results == nil {
		results = []knowledge.SearchResult{}
	}
	writeJSON(w, results)
}

func (s *Server) requireKnowledge(w http.ResponseWriter) bool {
	if s.knowledge != nil {
		return true
	}
	writeError(w, fmt.Errorf("知识库模块不可用，请确认 lark-cli 已安装"), http.StatusServiceUnavailable)
	return false
}

func parseKnowledgeSourceID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("无效的知识源 ID")
	}
	return id, nil
}
