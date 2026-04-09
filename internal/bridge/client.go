package bridge

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client gerencia uma sessão autenticada do bridge e as rotinas de manutenção em segundo plano.
type Client struct {
	mu         sync.RWMutex
	cfg        Config
	httpClient *http.Client
	token      string
	status     Status
	logger     *ringLogger
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewClient normaliza a configuração, valida os campos obrigatórios e prepara o estado HTTP da sessão.
func NewClient(cfg Config, logger *ringLogger) (*Client, error) {
	cfg = withDefaults(cfg)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}, //nolint:gosec
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   25 * time.Second,
			Transport: tr,
		},
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
		status: Status{
			BaseURL:  cfg.BaseURL,
			BridgeID: cfg.BridgeID,
		},
	}, nil
}

// Connect autentica uma vez e inicia o loop de manutenção de heartbeat e renovação de token.
func (c *Client) Connect() error {
	c.logger.Info("Iniciando autenticação com o Defense IA Bridge...")
	if err := c.authorize(); err != nil {
		c.setError(err)
		return err
	}
	c.logger.Info("Autenticação concluída com sucesso.")
	go c.maintenanceLoop()
	return nil
}

// Disconnect cancela operações em andamento e marca a sessão como desconectada.
func (c *Client) Disconnect() {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
	c.status.Connected = false
}

// Status retorna uma cópia do estado protegida pelo mutex do client.
func (c *Client) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// maintenanceLoop mantém a sessão saudável até que o contexto do client seja cancelado.
func (c *Client) maintenanceLoop() {
	heartbeatTicker := time.NewTicker(time.Duration(c.cfg.HeartbeatIntervalSec) * time.Second)
	tokenTicker := time.NewTicker(time.Duration(c.cfg.TokenRefreshMinutes) * time.Minute)
	defer heartbeatTicker.Stop()
	defer tokenTicker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-heartbeatTicker.C:
			if err := c.withAuthRetry(c.heartbeat); err != nil {
				c.setError(fmt.Errorf("heartbeat failed: %w", err))
				c.logger.Error("Heartbeat falhou: " + err.Error())
			}
		case <-tokenTicker.C:
			if err := c.withAuthRetry(c.updateToken); err != nil {
				c.setError(fmt.Errorf("token update failed: %w", err))
				c.logger.Error("Renovação de token falhou: " + err.Error())
			}
		}
	}
}

// SendCustomEvent monta o payload canônico esperado pela API e mescla os campos extras válidos.
func (c *Client) SendCustomEvent(input CustomEventInput) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"eventId":         newPseudoUUID(),
		"eventTime":       eventTimeOrNow(input.EventTimeUnix),
		"eventSourceCode": valueOrDefault(input.EventSourceCode, c.cfg.EventSourceCode),
		"eventSourceName": valueOrDefault(input.EventSourceName, c.cfg.EventSourceName),
		"eventTypeCode":   strings.TrimSpace(input.EventTypeCode),
		"eventTypeName":   valueOrDefault(input.EventTypeName, "Custom Event"),
		"remark":          strings.TrimSpace(input.Message),
	}

	if payload["eventTypeCode"] == "" {
		return nil, errors.New("eventTypeCode é obrigatório")
	}

	for k, v := range input.ExtraFields {
		if strings.TrimSpace(k) == "" {
			continue
		}
		payload[k] = v
	}

	var resp map[string]interface{}
	err := c.withAuthRetry(func() error {
		body, _, err := c.doJSON(http.MethodPost, c.cfg.EventPushPath, payload, true)
		if err != nil {
			return err
		}
		if len(body) == 0 {
			resp = map[string]interface{}{"status": "ok"}
			return nil
		}
		return json.Unmarshal(body, &resp)
	})
	if err != nil {
		return nil, err
	}

	c.logger.Info(fmt.Sprintf("Evento enviado com sucesso. type=%s source=%s", payload["eventTypeCode"], payload["eventSourceCode"]))
	return resp, nil
}

// LoadBridgeConfig resolve o template do endpoint configurado usando o Bridge ID atual.
func (c *Client) LoadBridgeConfig() (map[string]interface{}, error) {
	if strings.TrimSpace(c.cfg.BridgeID) == "" {
		return nil, errors.New("bridgeId não informado")
	}

	path := strings.ReplaceAll(c.cfg.BridgeConfigPath, "{id}", c.cfg.BridgeID)
	var resp map[string]interface{}
	err := c.withAuthRetry(func() error {
		body, _, err := c.doJSON(http.MethodGet, path, nil, true)
		if err != nil {
			return err
		}
		if len(body) == 0 {
			resp = map[string]interface{}{}
			return nil
		}
		return json.Unmarshal(body, &resp)
	})
	if err != nil {
		return nil, err
	}

	c.logger.Info("Configuração do Bridge consultada com sucesso.")
	return resp, nil
}

// authorize executa o handshake com AK/SK e armazena o token e o estado resultantes.
func (c *Client) authorize() error {
	timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
	signature := computeHMACSHA256(timestamp, c.cfg.SecretKey)

	payload := map[string]string{
		"accessKey": c.cfg.AccessKey,
		"signature": signature,
		"timestamp": timestamp,
	}

	body, headers, err := c.doJSON(http.MethodPost, c.cfg.AuthPath, payload, false)
	if err != nil {
		return err
	}

	token, err := extractToken(body, headers)
	if err != nil {
		return fmt.Errorf("authorize response sem token reconhecível: %w", err)
	}

	c.mu.Lock()
	c.token = token
	c.status.Connected = true
	c.status.LastAuthorizeAt = time.Now()
	c.status.TokenExpiresAt = time.Now().Add(30 * time.Minute)
	c.status.LastError = ""
	c.mu.Unlock()

	return nil
}

// heartbeat informa à API que a sessão continua ativa.
func (c *Client) heartbeat() error {
	_, _, err := c.doJSON(http.MethodPost, c.cfg.HeartbeatPath, map[string]interface{}{}, true)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.status.Connected = true
	c.status.LastHeartbeatAt = time.Now()
	c.status.LastError = ""
	c.mu.Unlock()

	c.logger.Debug("Heartbeat enviado com sucesso em %s", time.Now().Format(time.RFC3339))
	return nil
}

// updateToken renova o token da sessão quando a API expõe esse endpoint.
func (c *Client) updateToken() error {
	body, headers, err := c.doJSON(http.MethodPost, c.cfg.TokenUpdatePath, map[string]interface{}{}, true)
	if err != nil {
		return err
	}

	token, err := extractToken(body, headers)
	if err != nil {
		c.logger.Warn("Token update respondeu sem novo token. Mantendo token atual.")
		c.mu.Lock()
		c.status.LastTokenUpdate = time.Now()
		c.status.TokenExpiresAt = time.Now().Add(30 * time.Minute)
		c.status.LastError = ""
		c.mu.Unlock()
		return nil
	}

	c.mu.Lock()
	c.token = token
	c.status.LastTokenUpdate = time.Now()
	c.status.TokenExpiresAt = time.Now().Add(30 * time.Minute)
	c.status.LastError = ""
	c.mu.Unlock()

	c.logger.Info("Token renovado com sucesso.")
	return nil
}

// withAuthRetry tenta novamente uma única vez após um 401, reautenticando de forma transparente.
func (c *Client) withAuthRetry(fn func() error) error {
	err := fn()
	if err == nil {
		return nil
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		return err
	}

	c.logger.Warn("Recebido 401. Tentando reautenticar automaticamente...")
	if authErr := c.authorize(); authErr != nil {
		return fmt.Errorf("reauthorize failed: %w", authErr)
	}
	return fn()
}

// doJSON é o helper HTTP de baixo nível compartilhado por todas as operações do bridge.
func (c *Client) doJSON(method, path string, payload interface{}, withToken bool) ([]byte, http.Header, error) {
	url := buildURL(c.cfg.BaseURL, path)

	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(c.ctx, method, url, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if withToken {
		c.mu.RLock()
		token := c.token
		c.mu.RUnlock()
		if strings.TrimSpace(token) != "" {
			req.Header.Set("X-Subject-Token", token)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, &apiError{StatusCode: resp.StatusCode, Body: string(rawBody)}
	}

	return rawBody, resp.Header, nil
}

// setError atualiza o estado público sem tratar cancelamentos manuais como erro operacional.
func (c *Client) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status.LastError = err.Error()
	if errors.Is(err, context.Canceled) {
		return
	}
	c.status.Connected = false
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("status %d", e.StatusCode)
	}
	return fmt.Sprintf("status %d: %s", e.StatusCode, e.Body)
}

// withDefaults preenche endpoints opcionais e tempos padrão para manter a UI mais simples.
func withDefaults(cfg Config) Config {
	if cfg.AuthPath == "" {
		cfg.AuthPath = "/ecos/api/v1.1/account/authorize"
	}
	if cfg.HeartbeatPath == "" {
		cfg.HeartbeatPath = "/ecos/api/v1.1/account/heartbeat"
	}
	if cfg.TokenUpdatePath == "" {
		cfg.TokenUpdatePath = "/ecos/api/v1.1/account/token/update"
	}
	if cfg.EventPushPath == "" {
		cfg.EventPushPath = "/ecos/api/v1.1/bridge/event/push"
	}
	if cfg.BridgeConfigPath == "" {
		cfg.BridgeConfigPath = "/ecos/api/v1.1/bridge/{id}/config"
	}
	if cfg.HeartbeatIntervalSec <= 0 {
		cfg.HeartbeatIntervalSec = 30
	}
	if cfg.TokenRefreshMinutes <= 0 {
		cfg.TokenRefreshMinutes = 25
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
	cfg.SecretKey = strings.TrimSpace(cfg.SecretKey)
	cfg.BridgeID = strings.TrimSpace(cfg.BridgeID)
	cfg.EventSourceCode = strings.TrimSpace(cfg.EventSourceCode)
	cfg.EventSourceName = strings.TrimSpace(cfg.EventSourceName)
	return cfg
}

// validateConfig rejeita configurações que não têm como autenticar com sucesso.
func validateConfig(cfg Config) error {
	if cfg.BaseURL == "" {
		return errors.New("baseUrl é obrigatório")
	}
	if cfg.AccessKey == "" {
		return errors.New("accessKey é obrigatório")
	}
	if cfg.SecretKey == "" {
		return errors.New("secretKey é obrigatório")
	}
	return nil
}

// buildURL aceita tanto endpoints absolutos quanto paths relativos configurados na UI.
func buildURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if strings.HasPrefix(path, "/") {
		return baseURL + path
	}
	return baseURL + "/" + path
}

// computeHMACSHA256 assina o timestamp do authorize usando a secret key informada.
func computeHMACSHA256(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// extractToken suporta respostas com token tanto em header quanto no corpo JSON.
func extractToken(body []byte, headers http.Header) (string, error) {
	if token := strings.TrimSpace(headers.Get("X-Subject-Token")); token != "" {
		return token, nil
	}

	if len(body) == 0 {
		return "", errors.New("empty body")
	}

	var decoded interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}

	if token := findTokenRecursive(decoded); token != "" {
		return token, nil
	}

	return "", errors.New("token not found")
}

// findTokenRecursive procura nomes comuns de token em respostas JSON aninhadas.
func findTokenRecursive(v interface{}) string {
	switch vv := v.(type) {
	case map[string]interface{}:
		for _, key := range []string{"token", "subjectToken", "accessToken", "xSubjectToken"} {
			if raw, ok := vv[key]; ok {
				if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		for _, nested := range vv {
			if found := findTokenRecursive(nested); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, item := range vv {
			if found := findTokenRecursive(item); found != "" {
				return found
			}
		}
	}
	return ""
}

// eventTimeOrNow mantém o payload consistente quando a UI deixa o campo vazio.
func eventTimeOrNow(v int64) int64 {
	if v > 0 {
		return v
	}
	return time.Now().Unix()
}

// valueOrDefault trata strings vazias como ausentes para manter as regras do payload consistentes.
func valueOrDefault(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v != "" {
		return v
	}
	return strings.TrimSpace(fallback)
}

// newPseudoUUID gera um identificador no formato de UUID sem adicionar dependência externa.
func newPseudoUUID() string {
	now := time.Now().UTC().Format("20060102150405.000000000")
	sum := sha256.Sum256([]byte(now))
	raw := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", raw[0:8], raw[8:12], raw[12:16], raw[16:20], raw[20:32])
}
