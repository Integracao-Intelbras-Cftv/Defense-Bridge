package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"defense-bridge-client/internal/bridge"
)

//go:embed assets/templates/* assets/static/*
var assets embed.FS

// App conecta a UI embarcada ao serviço de bridge usado pela API local.
type App struct {
	templates *template.Template
	bridgeSvc *bridge.Service
	staticFS  http.Handler
}

type pageData struct {
	Title string
}

// New prepara os templates, os assets estáticos e o serviço de bridge em memória.
func New() (*App, error) {
	tpl, err := template.ParseFS(assets, "assets/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	staticSub, err := fs.Sub(assets, "assets/static")
	if err != nil {
		return nil, fmt.Errorf("static fs: %w", err)
	}

	return &App{
		templates: tpl,
		bridgeSvc: bridge.NewService(),
		staticFS:  http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))),
	}, nil
}

// Routes registra a página HTML principal e os endpoints JSON consumidos pelo front-end.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.Handle("/static/", a.staticFS)

	mux.HandleFunc("/api/connect", a.handleConnect)
	mux.HandleFunc("/api/disconnect", a.handleDisconnect)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/logs", a.handleLogs)
	mux.HandleFunc("/api/bridge-config", a.handleBridgeConfig)
	mux.HandleFunc("/api/send-event", a.handleSendEvent)

	return mux
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	if err := a.templates.ExecuteTemplate(w, "index.html", pageData{Title: "Defense Bridge Client"}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	var cfg bridge.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		respondError(w, http.StatusBadRequest, "json inválido")
		return
	}

	if err := a.bridgeSvc.Connect(cfg); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Conectado com sucesso.",
		"status":  a.bridgeSvc.Status(),
	})
}

func (a *App) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	a.bridgeSvc.Disconnect()
	respondJSON(w, http.StatusOK, map[string]string{"message": "Sessão encerrada."})
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, a.bridgeSvc.Status())
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, a.bridgeSvc.Logs())
}

func (a *App) handleBridgeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	payload, err := a.bridgeSvc.LoadBridgeConfig()
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, payload)
}

func (a *App) handleSendEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "método não permitido")
		return
	}

	var payload struct {
		EventTypeCode   string `json:"eventTypeCode"`
		EventTypeName   string `json:"eventTypeName"`
		EventSourceCode string `json:"eventSourceCode"`
		EventSourceName string `json:"eventSourceName"`
		Message         string `json:"message"`
		EventTimeUnix   int64  `json:"eventTimeUnix"`
		ExtraJSON       string `json:"extraJson"`
	}
	var extraFields map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "json inválido")
		return
	}

	if strings.TrimSpace(payload.ExtraJSON) != "" {
		// Aceita apenas um objeto aqui para que extraJson estenda o payload
		// do evento, em vez de substituir sua estrutura obrigatória.
		if err := json.Unmarshal([]byte(payload.ExtraJSON), &extraFields); err != nil {
			respondError(w, http.StatusBadRequest, "extraJson inválido: informe um objeto JSON")
			return
		}
	}

	resp, err := a.bridgeSvc.SendCustomEvent(bridge.CustomEventInput{
		EventTypeCode:   payload.EventTypeCode,
		EventTypeName:   payload.EventTypeName,
		EventSourceCode: payload.EventSourceCode,
		EventSourceName: payload.EventSourceName,
		Message:         payload.Message,
		EventTimeUnix:   payload.EventTimeUnix,
		ExtraFields:     extraFields,
	})
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Evento enviado com sucesso.",
		"response": resp,
	})
}

// respondJSON mantém o tratamento de headers e status consistente em toda a API local.
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// respondError preserva o contrato de erro esperado pelo front-end.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
