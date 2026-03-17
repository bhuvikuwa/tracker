package ipc

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"

	"ktracker/internal/protocol"

	"github.com/Microsoft/go-winio"
	"github.com/sirupsen/logrus"
)

const pipeName = `\\.\pipe\KTrackerLogin`

// PipeServer listens on a named pipe for login data from a second instance
type PipeServer struct {
	listener net.Listener
	callback func(protocol.LoginData)
	running  bool
	mu       sync.Mutex
}

// NewPipeServer creates a new pipe server
func NewPipeServer() *PipeServer {
	return &PipeServer{}
}

// SetLoginCallback sets the callback invoked when login data is received
func (ps *PipeServer) SetLoginCallback(fn func(protocol.LoginData)) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.callback = fn
}

// Start creates the named pipe listener and accepts connections in a goroutine
func (ps *PipeServer) Start() error {
	ps.mu.Lock()
	if ps.running {
		ps.mu.Unlock()
		return nil
	}
	ps.mu.Unlock()

	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		return fmt.Errorf("failed to create named pipe: %w", err)
	}

	ps.mu.Lock()
	ps.listener = listener
	ps.running = true
	ps.mu.Unlock()

	logrus.Infof("Named pipe server started on %s", pipeName)

	go ps.acceptLoop()

	return nil
}

// acceptLoop accepts connections and reads login data
func (ps *PipeServer) acceptLoop() {
	for {
		conn, err := ps.listener.Accept()
		if err != nil {
			ps.mu.Lock()
			running := ps.running
			ps.mu.Unlock()
			if !running {
				return // server was stopped
			}
			logrus.Warnf("Pipe accept error: %v", err)
			continue
		}

		go ps.handleConnection(conn)
	}
}

// handleConnection reads JSON LoginData from a pipe connection
func (ps *PipeServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	data, err := io.ReadAll(conn)
	if err != nil {
		logrus.Warnf("Pipe read error: %v", err)
		return
	}

	var loginData protocol.LoginData
	if err := json.Unmarshal(data, &loginData); err != nil {
		logrus.Warnf("Pipe JSON decode error: %v", err)
		return
	}

	logrus.Infof("Received login data via pipe: email=%s, app_code=%s", loginData.Email, loginData.AppCode)

	ps.mu.Lock()
	cb := ps.callback
	ps.mu.Unlock()

	if cb != nil {
		cb(loginData)
	}
}

// Stop closes the pipe listener
func (ps *PipeServer) Stop() error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if !ps.running {
		return nil
	}

	ps.running = false
	if ps.listener != nil {
		logrus.Info("Stopping named pipe server")
		return ps.listener.Close()
	}
	return nil
}

// SendLoginData connects to the named pipe and writes login data (used by 2nd instance)
func SendLoginData(data *protocol.LoginData) error {
	conn, err := winio.DialPipe(pipeName, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to pipe: %w", err)
	}
	defer conn.Close()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %w", err)
	}

	if _, err := conn.Write(jsonData); err != nil {
		return fmt.Errorf("failed to write to pipe: %w", err)
	}

	logrus.Infof("Sent login data via pipe: email=%s", data.Email)
	return nil
}
