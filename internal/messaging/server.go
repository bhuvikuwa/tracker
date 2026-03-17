package messaging

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ktracker/internal/config"

	"github.com/sirupsen/logrus"
)

// LoginMessage represents a login message from the web app
type LoginMessage struct {
	Username       string `json:"username"`
	AppCode        string `json:"app_code"`
	Action         string `json:"action"` // "login" or "logout"
	Email          string `json:"email"`
	Screenshots    int    `json:"screenshots"`
	ScreenshotTime int    `json:"screenshots_timing"`
	Logout         int    `json:"logout"`
	ExitApp        int    `json:"exit_app"`
}

// MessageCallback is called when a message is received
type MessageCallback func(msg LoginMessage)

// MessagingServer handles communication with the web application
type MessagingServer struct {
	cfg      *config.Config
	callback MessageCallback
	server   *http.Server
	mu       sync.RWMutex
	running  bool
}

// NewMessagingServer creates a new messaging server
func NewMessagingServer(cfg *config.Config) *MessagingServer {
	return &MessagingServer{
		cfg: cfg,
	}
}

// SetCallback sets the callback function for received messages
func (ms *MessagingServer) SetCallback(callback MessageCallback) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.callback = callback
}

// Start starts the messaging server
func (ms *MessagingServer) Start() error {
	ms.mu.Lock()
	if ms.running {
		ms.mu.Unlock()
		return nil
	}
	ms.running = true
	ms.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/login", ms.handleLogin)
	mux.HandleFunc("/status", ms.handleStatus)
	mux.HandleFunc("/ping", ms.handlePing)

	port := ms.cfg.Website.Port
	if port == 0 {
		port = 8080
	}

	ms.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: ms.enableCORS(mux),
	}

	logrus.Infof("Starting messaging server on port %d", port)
	
	// Start server in a goroutine
	go func() {
		if err := ms.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("Messaging server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the messaging server
func (ms *MessagingServer) Stop() error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	
	if !ms.running {
		return nil
	}
	
	ms.running = false
	
	if ms.server != nil {
		logrus.Info("Stopping messaging server")
		return ms.server.Close()
	}
	
	return nil
}

// enableCORS enables CORS for all requests
func (ms *MessagingServer) enableCORS(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		
		handler.ServeHTTP(w, r)
	})
}

// handleLogin handles login/logout requests from the web app
func (ms *MessagingServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg LoginMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		logrus.Errorf("Failed to decode login message: %v", err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	logrus.Infof("Received login message: action=%s, username=%s", msg.Action, msg.Username)

	ms.mu.RLock()
	callback := ms.callback
	ms.mu.RUnlock()

	if callback != nil {
		callback(msg)
	}

	response := map[string]interface{}{
		"status":  "success",
		"message": "Message received",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStatus returns the current app status
func (ms *MessagingServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "running",
		"message":   "KTracker is running",
		"timestamp": time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handlePing handles ping requests
func (ms *MessagingServer) handlePing(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "pong",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}