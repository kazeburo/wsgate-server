package dumper

import (
	"bytes"
	"encoding/hex"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Dumper dumper struct
type Dumper struct {
	direction uint
	logger    *zap.Logger
	buf       *bytes.Buffer
	mu        *sync.RWMutex
}

// New new handler
func New(direction uint, logger *zap.Logger) *Dumper {
	d := &Dumper{
		direction: direction,
		logger:    logger,
		buf:       new(bytes.Buffer),
		mu:        new(sync.RWMutex),
	}
	return d
}

// Write to dump
func (d *Dumper) Write(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buf.Write(p)
	return len(p), nil
}

// Flush  flush buffer
func (d *Dumper) Flush() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.buf.Len() == 0 {
		return
	}
	hexdump := strings.Split(hex.Dump(d.buf.Bytes()), "\n")
	d.buf.Truncate(0)
	byteString := []string{}
	ascii := []string{}
	for _, hd := range hexdump {
		if hd == "" {
			continue
		}
		byteString = append(byteString, strings.TrimRight(strings.Replace(hd[10:58], "  ", " ", 1), " "))
		ascii = append(ascii, hd[61:len(hd)-1])
	}
	d.logger.Info("dump",
		zap.Uint("direction", d.direction),
		zap.String("hex", strings.Join(byteString, " ")),
		zap.String("ascii", strings.Join(ascii, "")),
	)
}
