package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/initializer"
	"github.com/youdisn/lark-ob/internal/memory"
)

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agent/status", s.agentStatus)
	mux.HandleFunc("GET /api/initialization/status", s.initializationStatus)
	mux.HandleFunc("GET /api/profile-maintenance/status", s.profileMaintenanceStatus)
	mux.HandleFunc("GET /api/chats/{id}/profile", s.chatProfile)
	mux.HandleFunc("POST /api/chats/{id}/profile/refresh", s.refreshChatProfile)
	mux.HandleFunc("GET /api/chats/{id}/memories", s.chatMemories)
	mux.HandleFunc("POST /api/chats/{id}/memories/extract", s.extractChatMemories)
	mux.HandleFunc("GET /api/memories", s.memoryWarehouse)
	mux.HandleFunc("POST /api/memories", s.createMemory)
	mux.HandleFunc("GET /api/memories/subjects", s.memorySubjects)
	mux.HandleFunc("PUT /api/memories/{id}", s.updateMemory)
	mux.HandleFunc("DELETE /api/memories/{id}", s.deleteMemory)
	mux.HandleFunc("POST /api/chats/{id}/recommendation", s.recommendReply)
	mux.HandleFunc("GET /api/knowledge/sources/{id}/summary", s.knowledgeSummary)
	mux.HandleFunc("POST /api/knowledge/sources/{id}/summary", s.summarizeKnowledge)
}

func (s *Server) memorySubjects(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	options, err := s.memories.SubjectOptions(r.Context())
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, options)
}

func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	request, ok := decodeManualMemory(w, r)
	if !ok {
		return
	}
	claim, err := s.memories.CreateManual(r.Context(), request)
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	s.broadcast()
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, claim)
}

func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, fmt.Errorf("无效的记忆 ID"), http.StatusBadRequest)
		return
	}
	request, ok := decodeManualMemory(w, r)
	if !ok {
		return
	}
	claim, err := s.memories.UpdateManual(r.Context(), id, request)
	if err != nil {
		if err == sql.ErrNoRows {
			writeError(w, fmt.Errorf("只有手动记忆可以编辑"), http.StatusNotFound)
			return
		}
		writeError(w, err, http.StatusBadRequest)
		return
	}
	s.broadcast()
	writeJSON(w, claim)
}

func decodeManualMemory(w http.ResponseWriter, r *http.Request) (memory.ManualRequest, bool) {
	var request memory.ManualRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, fmt.Errorf("无效的记忆内容: %w", err), http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func (s *Server) refreshChatProfile(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgent(w) {
		return
	}
	if s.impressions == nil {
		writeError(w, fmt.Errorf("人物印象模块未启用"), http.StatusServiceUnavailable)
		return
	}
	lookbackDays := 30
	if s.initializer != nil && s.initializer.Status().LookbackDays > 0 {
		lookbackDays = s.initializer.Status().LookbackDays
	}
	bundle, err := s.impressions.RefreshPerson(r.Context(), r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("personId")), lookbackDays)
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	s.broadcast()
	writeJSON(w, bundle)
}

func (s *Server) profileMaintenanceStatus(w http.ResponseWriter, _ *http.Request) {
	if s.maintenance == nil {
		writeJSON(w, map[string]any{"phase": "disabled"})
		return
	}
	writeJSON(w, s.maintenance.Status())
}

func (s *Server) chatProfile(w http.ResponseWriter, r *http.Request) {
	if s.impressions == nil {
		writeError(w, fmt.Errorf("人物印象模块未启用"), http.StatusServiceUnavailable)
		return
	}
	bundle, err := s.impressions.Bundle(r.Context(), r.PathValue("id"), strings.TrimSpace(r.URL.Query().Get("personId")))
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, bundle)
}

func (s *Server) memoryWarehouse(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	warehouse, err := s.memories.Warehouse(r.Context(), memory.ListRequest{
		SubjectType: r.URL.Query().Get("subjectType"), SourceChatType: r.URL.Query().Get("sourceChatType"),
		Query: r.URL.Query().Get("q"), Limit: limit, Offset: offset,
	})
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	if warehouse.Items == nil {
		warehouse.Items = []memory.Claim{}
	}
	writeJSON(w, warehouse)
}

func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, fmt.Errorf("无效的记忆 ID"), http.StatusBadRequest)
		return
	}
	if err := s.memories.Delete(r.Context(), id); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, fmt.Errorf("记忆不存在"), http.StatusNotFound)
			return
		}
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	s.broadcast()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) initializationStatus(w http.ResponseWriter, _ *http.Request) {
	if s.initializer == nil {
		writeJSON(w, initializer.Status{Phase: "disabled", Error: "初始化模块未启用"})
		return
	}
	writeJSON(w, s.initializer.Status())
}

func (s *Server) agentStatus(w http.ResponseWriter, _ *http.Request) {
	if s.agent == nil {
		writeJSON(w, agent.Status{Framework: "CloudWeGo Eino", Model: "grok-4.5", Error: "Agent 模块未初始化"})
		return
	}
	writeJSON(w, s.agent.Status())
}

func (s *Server) chatMemories(w http.ResponseWriter, r *http.Request) {
	if s.memories == nil {
		writeError(w, fmt.Errorf("记忆模块不可用"), http.StatusServiceUnavailable)
		return
	}
	claims, err := s.memories.ListChat(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusInternalServerError)
		return
	}
	if claims == nil {
		claims = []memory.Claim{}
	}
	writeJSON(w, claims)
}

func (s *Server) extractChatMemories(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgent(w) {
		return
	}
	result, err := s.agent.ExtractChatMemory(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	s.broadcast()
	writeJSON(w, result)
}

func (s *Server) recommendReply(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgent(w) {
		return
	}
	var request struct {
		TargetMessageID string             `json:"targetMessageId"`
		Instruction     string             `json:"instruction"`
		Previous        []agent.Suggestion `json:"previousSuggestions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
		writeError(w, fmt.Errorf("无效的回复目标: %w", err), http.StatusBadRequest)
		return
	}
	result, err := s.agent.RecommendReplyWithInstruction(r.Context(), r.PathValue("id"), request.TargetMessageID, request.Instruction, request.Previous)
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func (s *Server) knowledgeSummary(w http.ResponseWriter, r *http.Request) {
	id, err := parseKnowledgeSourceID(r.PathValue("id"))
	if err != nil {
		writeError(w, err, http.StatusBadRequest)
		return
	}
	summary, err := s.store.GetKnowledgeSummary(r.Context(), id)
	if err != nil {
		writeError(w, err, http.StatusNotFound)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) summarizeKnowledge(w http.ResponseWriter, r *http.Request) {
	if !s.requireAgent(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, fmt.Errorf("无效的知识源 ID"), http.StatusBadRequest)
		return
	}
	summary, err := s.agent.SummarizeKnowledge(r.Context(), id)
	if err != nil {
		writeError(w, err, http.StatusBadGateway)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) requireAgent(w http.ResponseWriter) bool {
	if s.agent != nil && s.agent.Status().Enabled {
		return true
	}
	message := "Agent 未启用"
	if s.agent != nil && s.agent.Status().Error != "" {
		message += ": " + s.agent.Status().Error
	}
	writeError(w, fmt.Errorf("%s", message), http.StatusServiceUnavailable)
	return false
}
