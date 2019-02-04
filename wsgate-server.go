package main

import (
	"bufio"
	"crypto/rsa"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	ss "github.com/lestrrat/go-server-starter-listener"
	"go.uber.org/zap"
)

var (
	Version string

	listen           = flag.String("listen", "127.0.0.1:8086", "Address to listen to.")
	dialTimeout      = flag.Duration("dial_timeout", 10*time.Second, "Dial timeout.")
	handshakeTimeout = flag.Duration("handshake_timeout", 10*time.Second, "Handshake timeout.")
	writeTimeout     = flag.Duration("write_timeout", 10*time.Second, "Write timeout.")
	showVersion      = flag.Bool("version", false, "show version")
	mapFile          = flag.String("map", "", "path and proxy host mapping file")
	publicKeyFile    = flag.String("public-key", "", "public key for signing auth header")

	upgrader  websocket.Upgrader
	mapping   map[string]string
	verifyKey *rsa.PublicKey
)

func handleHello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK\n"))
}

func handleProxy(w http.ResponseWriter, r *http.Request, logger *zap.Logger) {
	vars := mux.Vars(r)
	proxyDest := vars["dest"]
	upstream := ""
	readLen := int64(0)
	writeLen := int64(0)
	hasError := false
	disconnectAt := ""

	logger = logger.With(
		zap.String("user-email", r.Header.Get("X-Goog-Authenticated-User-Email")),
		zap.String("x-forwarded-for", r.Header.Get("X-Forwarded-For")),
		zap.String("remote-addr", r.RemoteAddr),
		zap.String("destination", proxyDest),
	)

	if *publicKeyFile != "" {
		tokenString := r.Header.Get("Authorization")
		if tokenString == "" {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
		claims := &jwt.StandardClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			return verifyKey, nil
		})

		if err != nil {
			http.Error(w, fmt.Sprintf("Token is invalid: %v", err), http.StatusUnauthorized)
			return
		}
		if !token.Valid {
			http.Error(w, fmt.Sprintf("Token is invalid"), http.StatusUnauthorized)
			return
		}
		if claims.Valid() != nil {
			http.Error(w, fmt.Sprintf("Invalid claims: %v", claims.Valid()), http.StatusUnauthorized)
			return
		}
	}
	upstream, ok := mapping[proxyDest]
	if !ok {
		hasError = true
		logger.Warn("No map found")
		http.Error(w, fmt.Sprintf("Not found: %s", proxyDest), 404)
		return
	}

	logger = logger.With(zap.String("upstream", upstream))

	s, err := net.DialTimeout("tcp", upstream, *dialTimeout)

	if err != nil {
		hasError = true
		logger.Warn("DialTimeout", zap.Error(err))
		http.Error(w, fmt.Sprintf("Could not connect upstream: %v", err), 500)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
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

func printVersion() {
	fmt.Printf(`wsgate-server %s
Compiler: %s %s
`,
		Version,
		runtime.Compiler,
		runtime.Version())
}
func main() {
	flag.Parse()

	if *showVersion {
		printVersion()
		return
	}

	logger, _ := zap.NewProduction()

	r := regexp.MustCompile(`^ *#`)
	mapping = make(map[string]string)
	if *mapFile != "" {
		f, err := os.Open(*mapFile)
		if err != nil {
			logger.Fatal("Failed to open mapFile", zap.Error(err))
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			if r.MatchString(s.Text()) {
				continue
			}
			l := strings.SplitN(s.Text(), ",", 2)
			if len(l) != 2 {
				logger.Fatal("Invalid line",
					zap.String("mapFile", *mapFile),
					zap.String("line", s.Text()))
			}
			logger.Info("Created map",
				zap.String("from", l[0]),
				zap.String("to", l[1]))
			mapping[l[0]] = l[1]
		}
	}

	if *publicKeyFile != "" {
		verifyBytes, err := ioutil.ReadFile(*publicKeyFile)
		if err != nil {
			logger.Fatal("Failed read pubkey", zap.Error(err))
		}
		verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
		if err != nil {
			logger.Fatal("Failed read pubkey", zap.Error(err))
		}
	}

	upgrader = websocket.Upgrader{
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: *handshakeTimeout,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	m := mux.NewRouter()
	m.HandleFunc("/", handleHello)
	m.HandleFunc("/live", handleHello)
	m.HandleFunc("/proxy/{dest}", func(w http.ResponseWriter, r *http.Request) {
		handleProxy(w, r, logger)
	})

	l, err := ss.NewListener()
	if l == nil || err != nil {
		// Fallback if not running under Server::Starter
		l, err = net.Listen("tcp", *listen)
		if err != nil {
			logger.Fatal("Failed to listen to port", zap.String("listen", *listen))
		}
	}

	s := &http.Server{
		Handler:        m,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	s.Serve(l)
}
