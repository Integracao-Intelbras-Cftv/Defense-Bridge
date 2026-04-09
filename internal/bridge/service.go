package bridge

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Service mantém a instância ativa do client e expõe uma fachada segura para concorrência à camada HTTP.
type Service struct {
	mu     sync.RWMutex
	client *Client
	logs   *ringLogger
}

// NewService inicia sem sessão ativa e com um log em memória de tamanho limitado.
func NewService() *Service {
	return &Service{logs: newRingLogger(250)}
}

// Connect substitui qualquer sessão existente para que a UI sempre opere sobre um único client ativo.
func (s *Service) Connect(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		s.client.Disconnect()
		s.client = nil
	}

	client, err := NewClient(cfg, s.logs)
	if err != nil {
		return err
	}

	if err := client.Connect(); err != nil {
		return err
	}

	s.client = client
	return nil
}

// Disconnect encerra a sessão ativa e registra que o desligamento foi feito manualmente.
func (s *Service) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Disconnect()
		s.client = nil
		s.logs.Info("Sessão encerrada manualmente.")
	}
}

// Status retorna um retrato vazio quando ainda não existe sessão estabelecida.
func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.client == nil {
		return Status{}
	}
	return s.client.Status()
}

// Logs retorna uma cópia do buffer de diagnósticos em memória.
func (s *Service) Logs() []LogEntry {
	return s.logs.List()
}

// SendCustomEvent delega para o client ativo depois de garantir que existe uma sessão.
func (s *Service) SendCustomEvent(input CustomEventInput) (map[string]interface{}, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, errors.New("nenhuma sessão ativa")
	}

	return client.SendCustomEvent(input)
}

// LoadBridgeConfig consulta a configuração remota do bridge usando a sessão ativa.
func (s *Service) LoadBridgeConfig() (map[string]interface{}, error) {
	s.mu.RLock()
	client := s.client
	s.mu.RUnlock()

	if client == nil {
		return nil, errors.New("nenhuma sessão ativa")
	}

	return client.LoadBridgeConfig()
}

// ringLogger mantém apenas os logs mais recentes para limitar o uso de memória.
type ringLogger struct {
	mu      sync.RWMutex
	maxSize int
	entries []LogEntry
}

func newRingLogger(maxSize int) *ringLogger {
	return &ringLogger{maxSize: maxSize, entries: make([]LogEntry, 0, maxSize)}
}

func (r *ringLogger) add(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.entries) == r.maxSize {
		// Descarta a entrada mais antiga antes de adicionar a nova.
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:r.maxSize-1]
	}
	r.entries = append(r.entries, LogEntry{Time: time.Now(), Level: level, Message: msg})
}

func (r *ringLogger) Info(msg string)  { r.add("INFO", msg) }
func (r *ringLogger) Warn(msg string)  { r.add("WARN", msg) }
func (r *ringLogger) Error(msg string) { r.add("ERROR", msg) }
func (r *ringLogger) Debug(format string, args ...interface{}) {
	r.add("DEBUG", fmt.Sprintf(format, args...))
}

func (r *ringLogger) List() []LogEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]LogEntry, len(r.entries))
	copy(out, r.entries)
	return out
}
