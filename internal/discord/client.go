// Package discord talks to a locally running Discord client over its IPC
// socket (a Unix socket on Linux/macOS, a named pipe on Windows) to set Rich
// Presence. No account login or token is involved: Discord only trusts
// programs running on the same computer.
package discord

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	opHandshake uint32 = 0
	opFrame     uint32 = 1
	opClose     uint32 = 2
	opPing      uint32 = 3
	opPong      uint32 = 4

	maxFrame       = 1 << 20
	requestTimeout = 5 * time.Second
)

// Conn is what the presence engine needs from a Discord connection.
type Conn interface {
	// SetActivity shows the activity, or clears it when a is nil.
	SetActivity(a *Activity) error
	// Done is closed when the connection is lost.
	Done() <-chan struct{}
	Close() error
	Info() Info
}

type Info struct {
	Endpoint string // socket or pipe in use
	User     string // Discord user the client is signed in as
}

// NotFoundError means no Discord client could be reached.
type NotFoundError struct {
	Endpoints []string // sockets that were tried
	Details   []string // why each one failed
}

func (e *NotFoundError) Error() string {
	if len(e.Endpoints) == 0 {
		return "Discord is not running (no Discord IPC socket found)"
	}
	return fmt.Sprintf("could not talk to Discord (tried %d socket(s)): %s", len(e.Endpoints), strings.Join(e.Details, "; "))
}

// HandshakeError means Discord answered but refused us (for example an invalid
// application ID).
type HandshakeError struct {
	Code    int
	Message string
}

func (e *HandshakeError) Error() string {
	if e.Message == "" {
		return "Discord refused the connection"
	}
	return "Discord refused the connection: " + e.Message
}

// APIError means Discord understood the request but rejected it.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Discord rejected the request (%d): %s", e.Code, e.Message)
}

// Dialer opens one endpoint. Tests replace it; the default is platform specific.
type Dialer func(endpoint string) (net.Conn, error)

type Options struct {
	ClientID  string
	Endpoints []string      // default: Endpoints()
	Dial      Dialer        // default: the real socket/pipe
	Timeout   time.Duration // handshake timeout, default 5s
}

type frameResp struct {
	Cmd   string          `json:"cmd"`
	Evt   string          `json:"evt"`
	Nonce string          `json:"nonce"`
	Data  json.RawMessage `json:"data"`
}

// Client is a live connection to Discord.
type Client struct {
	nc       net.Conn
	endpoint string
	user     string

	wmu sync.Mutex // serialises writes

	pmu     sync.Mutex
	pending map[string]chan *frameResp

	done     chan struct{}
	closeErr error
	once     sync.Once
}

var _ Conn = (*Client)(nil)

// Connect finds a running Discord client and completes the handshake.
func Connect(ctx context.Context, o Options) (*Client, error) {
	if o.Dial == nil {
		o.Dial = dial
	}
	eps := o.Endpoints
	if eps == nil {
		eps = Endpoints()
	}
	if o.Timeout == 0 {
		o.Timeout = 5 * time.Second
	}

	nf := &NotFoundError{Endpoints: eps}
	for _, ep := range eps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nc, err := o.Dial(ep)
		if err != nil {
			nf.Details = append(nf.Details, fmt.Sprintf("%s: %v", ep, err))
			continue
		}
		c, err := handshake(nc, ep, o.ClientID, o.Timeout)
		if err != nil {
			_ = nc.Close()
			var he *HandshakeError
			if errors.As(err, &he) {
				return nil, err // another socket will not fix a bad application ID
			}
			nf.Details = append(nf.Details, fmt.Sprintf("%s: %v", ep, err))
			continue
		}
		go c.readLoop()
		return c, nil
	}
	return nil, nf
}

func handshake(nc net.Conn, ep, clientID string, timeout time.Duration) (*Client, error) {
	_ = nc.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = nc.SetDeadline(time.Time{}) }()

	hello, _ := json.Marshal(map[string]any{"v": 1, "client_id": clientID})
	if err := writeFrame(nc, opHandshake, hello); err != nil {
		return nil, err
	}
	for {
		op, payload, err := readFrame(nc)
		if err != nil {
			return nil, err
		}
		switch op {
		case opClose:
			code, msg := parseErrorData(payload)
			return nil, &HandshakeError{Code: code, Message: msg}
		case opPing:
			_ = writeFrame(nc, opPong, payload)
		case opFrame:
			var r frameResp
			if err := json.Unmarshal(payload, &r); err != nil {
				return nil, fmt.Errorf("unexpected reply from Discord: %w", err)
			}
			switch r.Evt {
			case "ERROR":
				code, msg := parseErrorData(r.Data)
				return nil, &HandshakeError{Code: code, Message: msg}
			case "READY":
				return &Client{
					nc:       nc,
					endpoint: ep,
					user:     parseUser(r.Data),
					pending:  map[string]chan *frameResp{},
					done:     make(chan struct{}),
				}, nil
			}
		}
	}
}

func (c *Client) Info() Info { return Info{Endpoint: c.endpoint, User: c.user} }

func (c *Client) Done() <-chan struct{} { return c.done }

// Err says why the connection ended (nil while it is still open).
func (c *Client) Err() error {
	select {
	case <-c.done:
		return c.closeErr
	default:
		return nil
	}
}

func (c *Client) Close() error {
	c.fail(errors.New("closed"))
	return nil
}

func (c *Client) fail(err error) {
	c.once.Do(func() {
		c.closeErr = err
		_ = c.nc.Close()
		close(c.done)
	})
}

func (c *Client) readLoop() {
	for {
		op, payload, err := readFrame(c.nc)
		if err != nil {
			c.fail(err)
			return
		}
		switch op {
		case opPing:
			if err := c.write(opPong, payload); err != nil {
				c.fail(err)
				return
			}
		case opClose:
			_, msg := parseErrorData(payload)
			if msg == "" {
				msg = "Discord closed the connection"
			}
			c.fail(errors.New(msg))
			return
		case opFrame:
			var r frameResp
			if json.Unmarshal(payload, &r) != nil || r.Nonce == "" {
				continue // an event we did not ask for
			}
			c.pmu.Lock()
			ch := c.pending[r.Nonce]
			c.pmu.Unlock()
			if ch != nil {
				ch <- &r
			}
		}
	}
}

func (c *Client) write(op uint32, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.nc.SetWriteDeadline(time.Now().Add(requestTimeout))
	return writeFrame(c.nc, op, payload)
}

// SetActivity shows a, or clears the presence when a is nil.
func (c *Client) SetActivity(a *Activity) error {
	args := map[string]any{"pid": os.Getpid()}
	if a != nil {
		args["activity"] = a
	}
	r, err := c.request("SET_ACTIVITY", args)
	if err != nil {
		return err
	}
	if r.Evt == "ERROR" {
		code, msg := parseErrorData(r.Data)
		return &APIError{Code: code, Message: msg}
	}
	return nil
}

func (c *Client) request(cmd string, args any) (*frameResp, error) {
	nonce := newNonce()
	ch := make(chan *frameResp, 1)
	c.pmu.Lock()
	c.pending[nonce] = ch
	c.pmu.Unlock()
	defer func() {
		c.pmu.Lock()
		delete(c.pending, nonce)
		c.pmu.Unlock()
	}()

	body, err := json.Marshal(map[string]any{"cmd": cmd, "args": args, "nonce": nonce})
	if err != nil {
		return nil, err
	}
	if err := c.write(opFrame, body); err != nil {
		c.fail(err)
		return nil, err
	}
	timer := time.NewTimer(requestTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r, nil
	case <-c.done:
		return nil, c.closeErr
	case <-timer.C:
		return nil, errors.New("timed out waiting for Discord to answer")
	}
}

func newNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func writeFrame(w io.Writer, op uint32, payload []byte) error {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[8:], payload)
	_, err := w.Write(buf) // one write so frames never interleave
	return err
}

func readFrame(r io.Reader) (uint32, []byte, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	op := binary.LittleEndian.Uint32(hdr[0:4])
	n := binary.LittleEndian.Uint32(hdr[4:8])
	if n > maxFrame {
		return 0, nil, fmt.Errorf("Discord sent an oversized frame (%d bytes)", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return op, buf, nil
}

func parseErrorData(raw []byte) (int, string) {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Code, e.Message
}

func parseUser(raw []byte) string {
	var d struct {
		User struct {
			Username   string `json:"username"`
			GlobalName string `json:"global_name"`
		} `json:"user"`
	}
	_ = json.Unmarshal(raw, &d)
	if d.User.GlobalName != "" {
		return d.User.GlobalName
	}
	return d.User.Username
}
