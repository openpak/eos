package main

// The SDK's websockets: the lobby socket (LobbyClient.LobbySocketURL), Stomp notifications and
// Epic Connect messaging (P2P signalling). All three are JSON text frames over one TLS connection
// that Traefik terminates, so the upgrade arrives here as plain HTTP/1.1.
//
// ponytail: RFC 6455 by hand rather than a dependency: the handshake, a length and an unmask.
// No fragmentation and no permessage-deflate (offered by libwebsockets, declined by not
// answering it, which leaves the client sending uncompressed frames).

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type wsConn struct {
	raw net.Conn
	rw  *bufio.ReadWriter
	mu  sync.Mutex // writers: the reader loop and pushes from other players
}

// upgrade completes the websocket handshake on r, or returns nil when r is not one.
func upgrade(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" {
		return nil, errors.New("not a websocket upgrade")
	}
	raw, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11")) // RFC 6455 §4.2.2
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n"
	// A client that offered subprotocols and gets none back may fail the connection.
	if p := r.Header.Get("Sec-WebSocket-Protocol"); p != "" {
		resp += "Sec-WebSocket-Protocol: " + strings.TrimSpace(strings.Split(p, ",")[0]) + "\r\n"
	}
	if _, err := rw.WriteString(resp + "\r\n"); err != nil {
		raw.Close()
		return nil, err
	}
	if err := rw.Flush(); err != nil {
		raw.Close()
		return nil, err
	}
	return &wsConn{raw: raw, rw: rw}, nil
}

func (c *wsConn) Close() error { return c.raw.Close() }

// send writes one unmasked server frame.
func (c *wsConn) send(op byte, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	hdr := []byte{0x80 | op}
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n < 1<<16:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	_ = c.raw.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.rw.Write(hdr); err != nil {
		return err
	}
	if _, err := c.rw.Write(payload); err != nil {
		return err
	}
	return c.rw.Flush()
}

func (c *wsConn) sendText(p []byte) error { return c.send(0x1, p) }

// next returns the next data frame, answering pings and stopping at a close. idle is how long
// the client may stay silent before it is pinged; a client that then stays silent is dropped.
func (c *wsConn) next(idle time.Duration) (byte, []byte, error) {
	pinged := false
	for {
		_ = c.raw.SetReadDeadline(time.Now().Add(idle))
		op, payload, err := readFrame(c.rw)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && !pinged {
				pinged = true
				if err := c.send(0x9, nil); err != nil {
					return 0, nil, err
				}
				continue
			}
			return 0, nil, err
		}
		pinged = false
		switch op {
		case 0x8:
			_ = c.send(0x8, nil)
			return 0, nil, io.EOF
		case 0x9:
			if err := c.send(0xa, payload); err != nil {
				return 0, nil, err
			}
		case 0xa:
		default:
			return op, payload, nil
		}
	}
}

// readFrame reads one client frame (always masked, RFC 6455 §5.3).
func readFrame(r io.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	op := h[0] & 0x0f
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	if n > 1<<20 {
		return 0, nil, errors.New("frame too large")
	}
	var mask [4]byte
	masked := h[1]&0x80 != 0
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	p := make([]byte, n)
	if _, err := io.ReadFull(r, p); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range p {
			p[i] ^= mask[i%4]
		}
	}
	return op, p, nil
}
