package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"time"

	"github.com/gorilla/mux"
	"github.com/kazeburo/wsgate-server/handler"
	"github.com/kazeburo/wsgate-server/mapping"
	"github.com/kazeburo/wsgate-server/publickey"
	ss "github.com/lestrrat/go-server-starter-listener"
	"go.uber.org/zap"
)

var (
	// Version wsgate-server version
	Version          string
	showVersion      = flag.Bool("version", false, "show version")
	listen           = flag.String("listen", "127.0.0.1:8086", "Address to listen to.")
	handshakeTimeout = flag.Duration("handshake_timeout", 10*time.Second, "Handshake timeout.")
	dialTimeout      = flag.Duration("dial_timeout", 10*time.Second, "Dial timeout.")
	writeTimeout     = flag.Duration("write_timeout", 10*time.Second, "Write timeout.")
	mapFile          = flag.String("map", "", "path and proxy host mapping file")
	publicKeyFile    = flag.String("public-key", "", "public key for signing auth header")
	dumpTCP          = flag.Uint("dump-tcp", 0, "Dump TCP. 0 = disable, 1 = src to dest, 2 = both")
)

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

	mp, err := mapping.New(*mapFile, logger)
	if err != nil {
		logger.Fatal("Failed init mapping", zap.Error(err))
	}

	pk, err := publickey.New(*publicKeyFile, logger)
	if err != nil {
		logger.Fatal("Failed init publickey", zap.Error(err))
	}

	proxyHandler, err := handler.New(
		*handshakeTimeout,
		*dialTimeout,
		*writeTimeout,
		mp,
		pk,
		*dumpTCP,
		logger,
	)
	if err != nil {
		logger.Fatal("Failed init handler", zap.Error(err))
	}

	m := mux.NewRouter()
	m.HandleFunc("/", proxyHandler.Hello())
	m.HandleFunc("/live", proxyHandler.Hello())
	m.HandleFunc("/proxy/{dest}", proxyHandler.Proxy())

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
