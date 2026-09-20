package ritalin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

func downloadProgress(r io.Reader, total, limit int64, emit Emit) ([]byte, error) {
	var out bytes.Buffer
	buf := make([]byte, 64<<10)
	last := time.Time{}
	for {
		n, e := r.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
			if int64(out.Len()) > limit {
				return nil, errors.New("下载超过大小限制")
			}
			if time.Since(last) > 200*time.Millisecond || int64(out.Len()) == total {
				if total > 0 {
					pct := min(100, int(int64(out.Len())*100/total))
					emit(fmt.Sprintf("↓ [%s%s] %3d%% · %.1f / %.1f MiB", strings.Repeat("█", pct/5), strings.Repeat("░", 20-pct/5), pct, float64(out.Len())/(1<<20), float64(total)/(1<<20)))
				} else {
					emit(fmt.Sprintf("↓ %.1f MiB", float64(out.Len())/(1<<20)))
				}
				last = time.Now()
			}
		}
		if e == io.EOF {
			return out.Bytes(), nil
		}
		if e != nil {
			return nil, e
		}
	}
}
