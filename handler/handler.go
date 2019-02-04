package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kazeburo/wsgate-server/mapping"
	"github.com/kazeburo/wsgate-server/publickey"
	"go.uber.org/zap"
)

// Handler handlers
type Handler struct {
	logger       *zap.Logger
	upgrader     websocket.Upgrader
	dialTimeout  time.Duration
	writeTimeout time.Duration
	mp           *mapping.Mapping
	pk           *publickey.Publickey
}

// New new handler
func New(
	handshakeTimeout time.Duration,
	dialTimeout time.Duration,
	writeTimeout time.Duration,
	mp *mapping.Mapping,
	pk *publickey.Publickey,
	logger *zap.Logger) (*Handler, error) {

	upgrader := websocket.Upgrader{
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: handshakeTimeout,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	return &Handler{
		logger:       logger,
		upgrader:     upgrader,
		dialTimeout:  dialTimeout,
		writeTimeout: writeTimeout,
		mp:           mp,
		pk:           pk,
	}, nil
}

// Hello hello handler
func (h *Handler) Hello() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK\n"))
	}
}

// Proxy proxy handler
func (h *Handler) Proxy() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		proxyDest := vars["dest"]
		upstream := ""
		readLen := int64(0)
		writeLen := int64(0)
		hasError := false
		disconnectAt := ""

		logger := h.logger.With(
			zap.String("user-email", r.Header.Get("X-Goog-Authenticated-User-Email")),
			zap.String("x-forwarded-for", r.Header.Get("X-Forwarded-For")),
			zap.String("remote-addr", r.RemoteAddr),
			zap.String("destination", proxyDest),
		)

		if h.pk.Enabled() {
			_, err := h.pk.Verify(r.Header.Get("Authorization"))
			if err != nil {
				logger.Warn("No authorize", zap.Error(err))
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
		}

		upstream, ok := h.mp.Get(proxyDest)
		if !ok {
			hasError = true
			logger.Warn("No map found")
			http.Error(w, fmt.Sprintf("Not found: %s", proxyDest), 404)
			return
		}

		logger = logger.With(zap.String("upstream", upstream))

		s, err := net.DialTimeout("tcp", upstream, h.dialTimeout)

		if err != nil {
			hasError = true
			logger.Warn("DialTimeout", zap.Error(err))
			http.Error(w, fmt.Sprintf("Could not connect upstream: %v", err), 500)
			return
		}

		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			hasError = true
			s.Close()
			logger.Warn("Failed to Upgrade", zap.Error(err))
			return
		}

		logger.Info("log", zap.String("status", "Connected"))

		defer func() {
			status := "Suceeded"
			if hasError {
				status = "Failed"
			}
			logger.Info("log",
				zap.String("status", status),
				zap.Int64("read", readLen),
				zap.Int64("write", writeLen),
				zap.String("disconnect_at", disconnectAt),
			)
		}()

		doneCh := make(chan bool)
		goClose := false

		// websocket -> server
		go func() {
			defer func() { doneCh <- true }()
			for {
				mt, r, err := conn.NextReader()
				if websocket.IsCloseError(err,
					websocket.CloseNormalClosure,   // Normal.
					websocket.CloseAbnormalClosure, // OpenSSH killed proxy client.
				) {
					return
				}
				if err != nil {
					if !goClose {
						logger.Warn("NextReader", zap.Error(err))
						hasError = true
					}
					if disconnectAt == "" {
						disconnectAt = "client_nextreader"
					}
					return
				}
				if mt != websocket.BinaryMessage {
					logger.Warn("BinaryMessage required", zap.Int("messageType", mt))
					hasError = true
					return
				}
				n, err := io.Copy(s, r)
				if err != nil {
					if !goClose {
						logger.Warn("Reading from websocket", zap.Error(err))
						hasError = true
					}
					if disconnectAt == "" {
						disconnectAt = "client_upstream_copy"
					}
					return
				}
				readLen += n
			}
		}()

		// server -> websocket
		go func() {
			defer func() { doneCh <- true }()
			for {
				b := make([]byte, 64*1024)
				n, err := s.Read(b)
				if err != nil {
					if !goClose && err != io.EOF {
						logger.Warn("Reading from dest", zap.Error(err))
						hasError = true
					}
					if disconnectAt == "" {
						disconnectAt = "upstream_read"
					}
					return
				}

				b = b[:n]

				if err := conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
					if !goClose {
						logger.Warn("WriteMessage", zap.Error(err))
						hasError = true
					}
					if disconnectAt == "" {
						disconnectAt = "client_write"
					}
					return
				}
				writeLen += int64(n)
			}
		}()

		<-doneCh
		goClose = true
		s.Close()
		conn.Close()
		<-doneCh

	}

}
