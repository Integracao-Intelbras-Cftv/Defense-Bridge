package bridge

import "time"

// Config reúne os parâmetros necessários para autenticar e manter uma sessão do bridge ativa.
type Config struct {
	BaseURL              string `json:"baseUrl"`
	BridgeID             string `json:"bridgeId"`
	AccessKey            string `json:"accessKey"`
	SecretKey            string `json:"secretKey"`
	EventSourceCode      string `json:"eventSourceCode"`
	EventSourceName      string `json:"eventSourceName"`
	AuthPath             string `json:"authPath"`
	HeartbeatPath        string `json:"heartbeatPath"`
	TokenUpdatePath      string `json:"tokenUpdatePath"`
	EventPushPath        string `json:"eventPushPath"`
	BridgeConfigPath     string `json:"bridgeConfigPath"`
	HeartbeatIntervalSec int    `json:"heartbeatIntervalSec"`
	TokenRefreshMinutes  int    `json:"tokenRefreshMinutes"`
	InsecureTLS          bool   `json:"insecureTls"`
}

// Status é o retrato público da sessão consumido pela interface web.
type Status struct {
	Connected       bool      `json:"connected"`
	BaseURL         string    `json:"baseUrl,omitempty"`
	BridgeID        string    `json:"bridgeId,omitempty"`
	LastAuthorizeAt time.Time `json:"lastAuthorizeAt,omitempty"`
	LastHeartbeatAt time.Time `json:"lastHeartbeatAt,omitempty"`
	LastTokenUpdate time.Time `json:"lastTokenUpdate,omitempty"`
	TokenExpiresAt  time.Time `json:"tokenExpiresAt,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
}

// CustomEventInput representa a parte controlada pelo usuário antes dos defaults do servidor.
type CustomEventInput struct {
	EventTypeCode   string                 `json:"eventTypeCode"`
	EventTypeName   string                 `json:"eventTypeName"`
	EventSourceCode string                 `json:"eventSourceCode"`
	EventSourceName string                 `json:"eventSourceName"`
	Message         string                 `json:"message"`
	EventTimeUnix   int64                  `json:"eventTimeUnix"`
	ExtraFields     map[string]interface{} `json:"extraFields"`
}

// LogEntry representa uma mensagem operacional armazenada no buffer de logs em memória.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}
