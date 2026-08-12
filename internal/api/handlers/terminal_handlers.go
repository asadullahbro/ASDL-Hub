package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/asdl/hub/internal/services"
)

type TerminalHandlers struct {
	terminal *services.TerminalService
	auth     *services.AuthService
}

func NewTerminalHandlers(t *services.TerminalService, auth *services.AuthService) *TerminalHandlers {
	return &TerminalHandlers{terminal: t, auth: auth}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // origin checked via JWT below
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// Message types from frontend
type TerminalMessage struct {
	Type string `json:"type"` // "input", "resize"
	Data string `json:"data"` // input chars
	Rows uint32 `json:"rows"`
	Cols uint32 `json:"cols"`
}

func (h *TerminalHandlers) Terminal(c *gin.Context) {
	nodeID := c.Param("id")

	// Validate JWT from query param (WebSocket can't send headers)
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}
	_, err := h.auth.ValidateToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	// Open SSH session
	session, err := h.terminal.OpenSession(nodeID)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("\r\n❌ "+err.Error()+"\r\n"))
		return
	}
	defer session.Client.Close()
	defer session.Session.Close()

	ws.WriteMessage(websocket.TextMessage, []byte("\r\n✅ Connected to node\r\n"))

	done := make(chan struct{})

	// SSH stdout → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := session.Stdout.Read(buf)
			if n > 0 {
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					break
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("SSH stdout read error: %v", err)
				}
				break
			}
		}
		close(done)
	}()

	// SSH stderr → WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := session.Stderr.Read(buf)
			if n > 0 {
				ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// WebSocket → SSH stdin
	ws.SetReadDeadline(time.Time{}) // no deadline
	for {
		select {
		case <-done:
			return
		default:
			_, msg, err := ws.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				return
			}

			var tmsg TerminalMessage
			if err := json.Unmarshal(msg, &tmsg); err != nil {
				// raw input — write directly
				session.Stdin.Write(msg)
				continue
			}

			switch tmsg.Type {
			case "input":
				session.Stdin.Write([]byte(tmsg.Data))
			case "resize":
				h.terminal.ResizeTerminal(session.Session, tmsg.Rows, tmsg.Cols)
			}
		}
	}
}