package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
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
	shutdownTimeout  = flag.Duration("shutdown_timeout", 86400*time.Second, "timeout to wait for all connections to be closed")
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

	wg := &sync.WaitGroup{}
	defer func() {
		c := make(chan struct{})
		go func() {
			defer close(c)
			wg.Wait()
		}()
		select {
		case <-c:
			logger.Info("All connections closed. Shutdown")
			return
		case <-time.After(*shutdownTimeout):
			logger.Info("Timeout, close some connections. Shutdown")
			return
		}
	}()

	m := mux.NewRouter()
	m.HandleFunc("/", proxyHandler.Hello())
	m.HandleFunc("/live", proxyHandler.Hello())
	m.HandleFunc("/proxy/{dest}", proxyHandler.Proxy(wg))

	s := &http.Server{
		Handler:        m,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM)
		<-sigChan
		logger.Info("Signal received. Start to shutdown")
		ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
		if es := s.Shutdown(ctx); es != nil {
			logger.Warn("Shutdown error", zap.Error(err))
		}
		cancel()
		close(idleConnsClosed)
		logger.Info("Waiting for all connections to be closed")
	}()

	l, err := ss.NewListener()
	if l == nil || err != nil {
		// Fallback if not running under Server::Starter
		l, err = net.Listen("tcp", *listen)
		if err != nil {
			logger.Fatal("Failed to listen to port", zap.String("listen", *listen))
		}
	}

	if err := s.Serve(l); err != http.ErrServerClosed {
		logger.Error("Error in Serve", zap.Error(err))
	}

	<-idleConnsClosed
}
