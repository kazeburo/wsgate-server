# wsgate-server - a websocket to tcp proxy/bridge server

```
[client]
|
| TCP
|
[wsgate-client] (https://github.com/kazeburo/wsgate-client)
|
| websocket (wss)
|
[reverse proxy] if required
|
| websocket (ws)
|
[wsgate-server] (https://github.com/kazeburo/wsgate-server)
|
| TCP
|
[server]
```

## Example

### wsgate-server

map-server.txt

```
mysql,127.0.0.1:3306
ssh,127.0.0.1:22
```
run server

```
$ wsgate-server --listen 0.0.0.0:8086 --map map-server.txt
```

### wsgate-client

map-client.txt

```
127.0.0.1:8306,https://example.com/proxy/mysql
127.0.0.1:8022,https://example.com/proxy/ssh
```

run client server

```
$ wsgate-client --map map-client.txt
```

### client

```
# mysql
$ mysql -h 127.0.0.1 --port 8306 --user ...

# ssh
ssh -p 8022 user@127.0.0.1
```

### Using go-sql-driver/mysql

It's able to use RegisterDial to connect wsgate-server.

```
mysql.RegisterDial("websocket", func(url string) (net.Conn, error) {
	wsURL := strings.Replace(url, "http", "ws", 1)
	wsConf, err := websocket.NewConfig(wsURL, url)
	if err != nil {
		log.Fatalf("NewConfig failed: %v", err)
	}
	conn, err := websocket.DialConfig(wsConf)
	if err != nil {
		log.Fatalf("Dial to %q fail: %v", url, err)
	}
	conn.PayloadType = websocket.BinaryFrame
	return conn, err
})

db, err := sql.Open("mysql", "yyyy:xxx@websocket(https://example.com/proxy/mysql)/test")
```

## Usage

```
Usage of ./wsgate-server:
  -dial_timeout duration
        Dial timeout. (default 10s)
  -handshake_timeout duration
        Handshake timeout. (default 10s)
  -listen string
        Address to listen to. (default "127.0.0.1:8086")
  -map string
        path and proxy host mapping file
  -public-key string
        public key for signing auth header
  -version
        show version
  -write_timeout duration
        Write timeout. (default 10s)
```
