package ritalin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
)

// Observe a copy only: original HTTP/WS bytes are forwarded unchanged.
type observedBody struct {
	io.ReadCloser
	ws           bool
	buf, message []byte
	observe      func(string, string)
}
type observedSocket struct {
	*observedBody
	io.Writer
}

func (r *observedBody) Read(p []byte) (int, error) {
	n, e := r.ReadCloser.Read(p)
	if n > 0 {
		r.feed(p[:n])
	}
	return n, e
}
func (r *observedBody) event(b []byte) {
	var e struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if json.Unmarshal(b, &e) != nil {
		return
	}
	switch e.Type {
	case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta":
		r.observe(e.Type, e.Delta)
	}
}
func (r *observedBody) feed(b []byte) {
	if len(r.buf)+len(b) > 8<<20 {
		r.buf = nil
		r.message = nil
		return
	}
	r.buf = append(r.buf, b...)
	if !r.ws {
		for {
			line, rest, ok := bytes.Cut(r.buf, []byte{'\n'})
			if !ok {
				return
			}
			r.buf = rest
			if bytes.HasPrefix(line, []byte("data:")) {
				r.event([]byte(strings.TrimSpace(string(line[5:]))))
			}
		}
	}
	for {
		if len(r.buf) < 2 {
			return
		}
		fin, op := r.buf[0]&128 != 0, r.buf[0]&15
		masked := r.buf[1]&128 != 0
		size := uint64(r.buf[1] & 127)
		offset := 2
		if size == 126 {
			if len(r.buf) < 4 {
				return
			}
			size = uint64(binary.BigEndian.Uint16(r.buf[2:4]))
			offset = 4
		} else if size == 127 {
			if len(r.buf) < 10 {
				return
			}
			size = binary.BigEndian.Uint64(r.buf[2:10])
			offset = 10
		}
		if size > 8<<20 {
			r.buf = nil
			r.message = nil
			return
		}
		var mask []byte
		if masked {
			if len(r.buf) < offset+4 {
				return
			}
			mask = r.buf[offset : offset+4]
			offset += 4
		}
		if uint64(len(r.buf)-offset) < size {
			return
		}
		payload := append([]byte(nil), r.buf[offset:offset+int(size)]...)
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		r.buf = r.buf[offset+int(size):]
		if op == 1 {
			r.message = payload
		} else if op == 0 {
			r.message = append(r.message, payload...)
		}
		if len(r.message) > 8<<20 {
			r.message = nil
		}
		if fin && (op == 1 || op == 0) {
			r.event(r.message)
			r.message = nil
		}
	}
}
