package handler

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/kazeburo/wsgate-server/internal/dumper"
	"github.com/kazeburo/wsgate-server/internal/mapping"
	"github.com/kazeburo/wsgate-server/internal/publickey"
	"go.uber.org/zap"
)

// BufferSize for coybuffer and websocket
const BufferSize = 256 * 1024

var (
	websocketUpstream   uint          = 1
	upstreamWebsocket   uint          = 2
	flushDumperInterval time.Duration = 300
)

// Handler handlers
type Handler struct {
	logger       *zap.Logger
	upgrader     websocket.Upgrader
	dialTimeout  time.Duration
	writeTimeout time.Duration
	mp           *mapping.Mapping
	pk           *publickey.Publickey
	dumpTCP      uint
	sq           *uint64
}

// New new handler
func New(
	handshakeTimeout time.Duration,
	dialTimeout time.Duration,
	writeTimeout time.Duration,
	enableCompression bool,
	mp *mapping.Mapping,
	pk *publickey.Publickey,
	dumpTCP uint,
	logger *zap.Logger) (*Handler, error) {

	upgrader := websocket.Upgrader{
		EnableCompression: enableCompression,
		ReadBufferSize:    BufferSize,
		WriteBufferSize:   BufferSize,
		HandshakeTimeout:  handshakeTimeout,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	seq := uint64(0)
	return &Handler{
		logger:       logger,
		upgrader:     upgrader,
		dialTimeout:  dialTimeout,
		writeTimeout: writeTimeout,
		mp:           mp,
		pk:           pk,
		dumpTCP:      dumpTCP,
		sq:           &seq,
	}, nil
}

func (h *Handler) GetSq() uint64 {
	return atomic.LoadUint64(h.sq)
}

// Hello hello handler
func (h *Handler) Hello() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK\n"))
	}
}

// Proxy proxy handler
func (h *Handler) Proxy(wg *sync.WaitGroup) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		wg.Add(1)
		defer wg.Done()

		vars := mux.Vars(r)
		proxyDest := vars["dest"]
		upstream := ""
		readLen := int64(0)
		writeLen := int64(0)
		hasError := false
		disconnectAt := ""

		logger := h.logger.With(
			zap.Uint64("seq", atomic.AddUint64(h.sq, 1)),
			zap.String("x-forwarded-for", r.Header.Get("X-Forwarded-For")),
			zap.String("remote-addr", r.RemoteAddr),
			zap.String("destination", proxyDest),
		)

		if h.pk.Enabled() {
			sub, err := h.pk.Verify(r.Header.Get("Authorization"))
			if err != nil {
				logger.Warn("Failed to authorize", zap.Error(err))
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}
			logger = logger.With(zap.String("user-email", sub))

		} else {
			logger = logger.With(zap.String("user-email", r.Header.Get("X-Goog-Authenticated-User-Email")))
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
		dr := dumper.New(websocketUpstream, logger)
		ds := dumper.New(upstreamWebsocket, logger)

		defer func() {
			dr.Flush()
			ds.Flush()
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

		ticker := time.NewTicker(flushDumperInterval * time.Millisecond)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-r.Context().Done():
					dr.Flush()
					ds.Flush()
					return
				case <-ticker.C:
					dr.Flush()
					ds.Flush()
				}
			}
		}()

		doneCh := make(chan bool)
		goClose := false

		// websocket -> server
		go func() {
			defer func() { doneCh <- true }()
			b := make([]byte, BufferSize)
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
				if h.dumpTCP > 0 {
					r = io.TeeReader(r, dr)
				}
				n, err := io.CopyBuffer(s, r, b)
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
			b := make([]byte, BufferSize)
			for {
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

				if h.dumpTCP > 1 {
					ds.Write(b)
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, b[:n]); err != nil {
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
