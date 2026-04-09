package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"defense-bridge-client/internal/server"
)

func main() {
	addr := envOrDefault("APP_ADDR", ":8080")

	app, err := server.New()
	if err != nil {
		log.Fatalf("failed to initialize server: %v", err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("Defense Bridge Client listening on http://localhost%s", normalizeAddr(addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// envOrDefault mantém os valores padrão de inicialização explícitos e fáceis de revisar.
func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// normalizeAddr mantém o log de inicialização legível quando o endereço vem como host:port.
func normalizeAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return ":8080"
}
