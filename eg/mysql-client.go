package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/net/websocket"
)

var (
	writeTimeout = flag.Duration("write_timeout", 10*time.Second, "Write timeout")
)

func main() {

	mysql.RegisterDial("websocket", func(url string) (net.Conn, error) {
		wsUrl := strings.Replace(url, "http", "ws", 1)
		log.Printf("connecting to %s", wsUrl)
		conn, err := websocket.Dial(wsUrl, "", url)
		if err != nil {
			log.Fatalf("Dial to %q fail: %v", url, err)
		}
		conn.PayloadType = websocket.BinaryFrame
		return conn, err
	})

	db, err := sql.Open("mysql", "root:root@websocket(http://127.0.0.1:8086/proxy/mysql)/mysql")
	if err != nil {
		log.Fatalf("open failed: %v", err)
	}
	rows, err := db.Query("SELECT * FROM time_zone")
	if err != nil {
		log.Fatalf(err.Error())
	}
	fmt.Printf("result: %v\n", rows)

}
