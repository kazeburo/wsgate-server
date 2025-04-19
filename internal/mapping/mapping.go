package mapping

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Mapping struct
type Mapping struct {
	m map[string]string
}

// New new mapping
func New(mapFile string, logger *zap.Logger) (*Mapping, error) {
	r := regexp.MustCompile(`^ *#`)
	m := make(map[string]string)
	if mapFile != "" {
		f, err := os.Open(mapFile)
		if err != nil {
			return nil, errors.Wrap(err, "Failed to open mapFile")
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			if r.MatchString(s.Text()) {
				continue
			}
			l := strings.SplitN(s.Text(), ",", 2)
			if len(l) != 2 {
				return nil, errors.Wrapf(err, "Invalid line: %s", s.Text())
			}
			logger.Info("Created map",
				zap.String("from", l[0]),
				zap.String("to", l[1]))
			m[l[0]] = l[1]
		}
	}
	return &Mapping{
		m: m,
	}, nil
}

// Get get mapping
func (mp *Mapping) Get(proxyDest string) (string, bool) {
	upstream, ok := mp.m[proxyDest]
	return upstream, ok
}

// Set mapping
func (mp *Mapping) Set(proxyDest string, upstream string) {
	mp.m[proxyDest] = upstream
}
