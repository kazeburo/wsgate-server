package main

import (
	"bufio"
	"crypto/rsa"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
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

func handleProxy(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	proxyDest := vars["dest"]
	upstream := ""
	readLen := int64(0)
	writeLen := int64(0)
	hasError := false

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
		log.Printf("No map for '%s' found", proxyDest)
		http.Error(w, fmt.Sprintf("Not found: %s", proxyDest), 404)
		return
	}

	s, err := net.DialTimeout("tcp", upstream, *dialTimeout)
	if err != nil {
		hasError = true
		log.Printf("DialTimeout: %v", err)
		http.Error(w, fmt.Sprintf("Could not connect upstream: %v", err), 500)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		hasError = true
		s.Close()
		log.Printf("Upgrade: %v", err)
		return
	}

	defer func() {
		status := "Suceeded"
		if hasError {
			status = "Failed"
		}
		log.Printf("status:%s dest:%s upstream:%s x-forwarded-for:%s remote_addr:%s read:%d write:%d",
			status, proxyDest, upstream, r.Header.Get("X-Forwarded-For"),
			r.RemoteAddr, readLen, writeLen)
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
					log.Printf("NextReader: %v", err)
					hasError = true
				}
				return
			}
			if mt != websocket.BinaryMessage {
				log.Printf("BinaryMessage required: %d", mt)
				hasError = true
				return
			}
			n, err := io.Copy(s, r)
			if err != nil {
				if !goClose {
					log.Printf("Reading from websocket: %v", err)
					hasError = true
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
					log.Printf("Reading from dest: %v", err)
					hasError = true
				}
				return
			}

			b = b[:n]

			if err := conn.WriteMessage(websocket.BinaryMessage, b); err != nil {
				if !goClose {
					log.Printf("WriteMessage: %v", err)
					hasError = true
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

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf(`anondb-wsgate %s
Compiler: %s %s
`,
			Version,
			runtime.Compiler,
			runtime.Version())
		return

	}

	r := regexp.MustCompile(`^ *#`)
	mapping = make(map[string]string)
	if *mapFile != "" {
		f, err := os.Open(*mapFile)
		if err != nil {
			log.Fatal(err)
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			if r.MatchString(s.Text()) {
				continue
			}
			l := strings.SplitN(s.Text(), ",", 2)
			if len(l) != 2 {
				log.Fatalf("Invalid line in %s: %s", *mapFile, s.Text())
			}
			log.Printf("Create map: %s => %s", l[0], l[1])
			mapping[l[0]] = l[1]
		}
	}

	if *publicKeyFile != "" {
		verifyBytes, err := ioutil.ReadFile(*publicKeyFile)
		if err != nil {
			log.Fatalf("Failed read pubkey: %v", err)
		}
		verifyKey, err = jwt.ParseRSAPublicKeyFromPEM(verifyBytes)
		if err != nil {
			log.Fatalf("Failed read pubkey: %v", err)
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
	m.HandleFunc("/proxy/{dest}", handleProxy)

	l, err := ss.NewListener()
	if l == nil || err != nil {
		// Fallback if not running under Server::Starter
		l, err = net.Listen("tcp", *listen)
		if err != nil {
			panic(fmt.Sprintf("Failed to listen to port %s", *listen))
		}
	}

	s := &http.Server{
		Handler:        m,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	log.Fatal(s.Serve(l))
}
