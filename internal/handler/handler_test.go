package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/gorilla/mux"
	"github.com/kazeburo/wsgate-server/internal/mapping"
	"github.com/kazeburo/wsgate-server/internal/publickey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHello(t *testing.T) {
	logger := zap.NewNop()
	h, err := New(
		10*time.Second,
		10*time.Second,
		10*time.Second,
		true,
		nil,
		nil,
		0,
		logger,
	)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()

	handler := h.Hello()
	handler(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK\n", rec.Body.String())
}

func createClient(wsAddr string, disableKeepalive bool) *http.Client {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				wsConf, err := websocket.NewConfig(
					fmt.Sprintf("ws://%s/proxy/dummy", wsAddr),
					fmt.Sprintf("http://%s/proxy/dummy", wsAddr),
				)
				if err != nil {
					return nil, err
				}
				conn, err := websocket.DialConfig(wsConf)
				if err != nil {
					return nil, err
				}
				conn.PayloadType = websocket.BinaryFrame
				return conn, nil
			},
			DisableKeepAlives: disableKeepalive,
		},
	}
	return client
}

func TestWebSocket(t *testing.T) {
	logger := zap.NewExample()

	// 空いているポートでテストサーバーを起動
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from dummy server"))
	})
	ts := httptest.NewServer(dummyHandler)
	defer ts.Close()
	addr := ts.Listener.Addr().String()
	t.Logf("dummy server address: %s", addr)
	mp, _ := mapping.New("", logger)
	mp.Set("dummy", addr)

	pk, _ := publickey.New("", time.Minute, logger)

	proxyHandler, err := New(
		10*time.Second,
		10*time.Second,
		10*time.Second,
		true,
		mp,
		pk,
		0,
		logger,
	)
	assert.NoError(t, err)

	wg := &sync.WaitGroup{}
	m := mux.NewRouter()
	m.HandleFunc("/proxy/{dest}", proxyHandler.Proxy(wg))

	ws := httptest.NewServer(m)
	defer ws.Close()

	// 割り当てられたポート番号を取得
	wsAddr := ws.Listener.Addr().String()
	t.Logf("wsAddr: %s", wsAddr)

	// http clientでの接続
	{
		client := createClient(wsAddr, false)
		for i := 0; i < 10; i++ {
			req, _ := http.NewRequest(http.MethodGet, "http://example/test", nil)
			resp, err := client.Do(req)
			assert.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			assert.Equal(t, "Hello from dummy server", string(body))
		}
		client.CloseIdleConnections()
	}
	{
		client := createClient(wsAddr, true)
		for i := 0; i < 3; i++ {
			req, _ := http.NewRequest(http.MethodGet, "http://example/test", nil)
			resp, err := client.Do(req)
			assert.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			assert.Equal(t, "Hello from dummy server", string(body))
		}
		client.CloseIdleConnections()
	}
	assert.Equal(t, uint64(4), proxyHandler.GetSq())
}
