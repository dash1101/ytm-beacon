package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

const readyFrame = `{"cmd":"DISPATCH","evt":"READY","data":{"user":{"username":"tester","global_name":"Tester"}},"nonce":null}`

// fakeDiscord plays the part of the Discord client on the other end of an
// in-memory pipe.
type rawFrame struct {
	op      uint32
	payload []byte
}

type fakeDiscord struct {
	t        *testing.T
	frames   chan map[string]any // every SET_ACTIVITY-style request received
	ops      chan rawFrame       // non-frame messages (PONG...) received
	hello    chan map[string]any
	onFrame  func(srv net.Conn, req map[string]any)
	serverEP chan net.Conn
}

func newFake(t *testing.T) *fakeDiscord {
	return &fakeDiscord{t: t, frames: make(chan map[string]any, 16), ops: make(chan rawFrame, 16), hello: make(chan map[string]any, 1), serverEP: make(chan net.Conn, 1)}
}

func (f *fakeDiscord) dial(string) (net.Conn, error) {
	client, srv := net.Pipe()
	f.serverEP <- srv
	go f.serve(srv)
	return client, nil
}

func (f *fakeDiscord) serve(srv net.Conn) {
	op, payload, err := readFrame(srv)
	if err != nil || op != opHandshake {
		return
	}
	var h map[string]any
	_ = json.Unmarshal(payload, &h)
	f.hello <- h
	if err := writeFrame(srv, opFrame, []byte(readyFrame)); err != nil {
		return
	}
	for {
		op, payload, err := readFrame(srv)
		if err != nil {
			return
		}
		if op != opFrame {
			f.ops <- rawFrame{op, payload}
			continue
		}
		var req map[string]any
		_ = json.Unmarshal(payload, &req)
		f.frames <- req
		if f.onFrame != nil {
			f.onFrame(srv, req)
			continue
		}
		reply, _ := json.Marshal(map[string]any{"cmd": req["cmd"], "evt": nil, "nonce": req["nonce"], "data": map[string]any{}})
		_ = writeFrame(srv, opFrame, reply)
	}
}

func connect(t *testing.T, f *fakeDiscord) *Client {
	t.Helper()
	c, err := Connect(context.Background(), Options{ClientID: "123456789012345678", Endpoints: []string{"fake"}, Dial: f.dial, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestConnectAndSetActivity(t *testing.T) {
	f := newFake(t)
	c := connect(t, f)

	h := <-f.hello
	if h["client_id"] != "123456789012345678" || h["v"] != float64(1) {
		t.Fatalf("bad handshake: %v", h)
	}
	if c.Info().User != "Tester" || c.Info().Endpoint != "fake" {
		t.Fatalf("bad info: %+v", c.Info())
	}

	act := &Activity{
		Type: TypeListening, Details: "Song", State: "Artist",
		Timestamps: &Timestamps{Start: 1000, End: 2000},
		Assets:     &Assets{LargeImage: "https://example.com/a.png", SmallImage: "play"},
		Buttons:    []Button{{Label: "Listen", URL: "https://music.youtube.com/watch?v=abc"}},
	}
	if err := c.SetActivity(act); err != nil {
		t.Fatalf("SetActivity: %v", err)
	}
	req := <-f.frames
	if req["cmd"] != "SET_ACTIVITY" || req["nonce"] == "" {
		t.Fatalf("bad request: %v", req)
	}
	args := req["args"].(map[string]any)
	if args["pid"] == nil {
		t.Error("pid missing")
	}
	a := args["activity"].(map[string]any)
	if a["type"] != float64(2) || a["details"] != "Song" || a["state"] != "Artist" {
		t.Errorf("bad activity: %v", a)
	}
	ts := a["timestamps"].(map[string]any)
	if ts["start"] != float64(1000) || ts["end"] != float64(2000) {
		t.Errorf("bad timestamps: %v", ts)
	}
	if _, has := a["status_display_type"]; has {
		t.Error("status_display_type should be omitted when unset")
	}
}

func TestClearSendsNoActivity(t *testing.T) {
	f := newFake(t)
	c := connect(t, f)
	if err := c.SetActivity(nil); err != nil {
		t.Fatal(err)
	}
	args := (<-f.frames)["args"].(map[string]any)
	if _, has := args["activity"]; has {
		t.Errorf("clearing must omit the activity, got %v", args)
	}
}

func TestSetActivityAPIError(t *testing.T) {
	f := newFake(t)
	f.onFrame = func(srv net.Conn, req map[string]any) {
		reply, _ := json.Marshal(map[string]any{"cmd": "SET_ACTIVITY", "evt": "ERROR", "nonce": req["nonce"], "data": map[string]any{"code": 4000, "message": "bad activity"}})
		_ = writeFrame(srv, opFrame, reply)
	}
	c := connect(t, f)
	err := c.SetActivity(&Activity{Details: "x"})
	var api *APIError
	if !errors.As(err, &api) || api.Code != 4000 || api.Message != "bad activity" {
		t.Fatalf("want APIError, got %v", err)
	}
	// An API error must not kill the connection.
	select {
	case <-c.Done():
		t.Fatal("connection closed after an API error")
	default:
	}
}

func TestHandshakeRejected(t *testing.T) {
	tried := 0
	dial := func(string) (net.Conn, error) {
		tried++
		client, srv := net.Pipe()
		go func() {
			_, _, _ = readFrame(srv)
			_ = writeFrame(srv, opClose, []byte(`{"code":4000,"message":"Invalid Client ID"}`))
		}()
		return client, nil
	}
	_, err := Connect(context.Background(), Options{ClientID: "1", Endpoints: []string{"a", "b"}, Dial: dial, Timeout: time.Second})
	var he *HandshakeError
	if !errors.As(err, &he) || he.Message != "Invalid Client ID" {
		t.Fatalf("want HandshakeError, got %v", err)
	}
	if tried != 1 {
		t.Errorf("should not try other sockets after a refusal, tried %d", tried)
	}
}

func TestNotFound(t *testing.T) {
	dial := func(ep string) (net.Conn, error) { return nil, errors.New("connection refused") }
	_, err := Connect(context.Background(), Options{ClientID: "1", Endpoints: []string{"a", "b"}, Dial: dial})
	var nf *NotFoundError
	if !errors.As(err, &nf) || len(nf.Details) != 2 {
		t.Fatalf("want NotFoundError with 2 details, got %v", err)
	}

	_, err = Connect(context.Background(), Options{ClientID: "1", Endpoints: []string{}, Dial: dial})
	if !errors.As(err, &nf) || nf.Error() != "Discord is not running (no Discord IPC socket found)" {
		t.Fatalf("unexpected error for no sockets: %v", err)
	}
}

func TestFallsThroughToNextSocket(t *testing.T) {
	f := newFake(t)
	dial := func(ep string) (net.Conn, error) {
		if ep == "dead" {
			return nil, errors.New("connection refused")
		}
		return f.dial(ep)
	}
	c, err := Connect(context.Background(), Options{ClientID: "1", Endpoints: []string{"dead", "alive"}, Dial: dial, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Info().Endpoint != "alive" {
		t.Errorf("endpoint = %q", c.Info().Endpoint)
	}
}

func TestAnswersPing(t *testing.T) {
	f := newFake(t)
	connect(t, f)
	srv := <-f.serverEP
	if err := writeFrame(srv, opPing, []byte(`{"n":1}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-f.ops:
		if got.op != opPong || string(got.payload) != `{"n":1}` {
			t.Fatalf("want PONG echo, got op=%d payload=%q", got.op, got.payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no PONG received")
	}
}

func TestDoneWhenDiscordCloses(t *testing.T) {
	f := newFake(t)
	c := connect(t, f)
	srv := <-f.serverEP
	_ = srv.Close()
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() not closed after Discord hung up")
	}
	if c.Err() == nil {
		t.Error("Err() should explain why")
	}
	if err := c.SetActivity(&Activity{Details: "x"}); err == nil {
		t.Error("SetActivity on a closed connection should fail")
	}
}

func TestFrameRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	go func() { _ = writeFrame(a, opFrame, []byte("hello")) }()
	op, p, err := readFrame(b)
	if err != nil || op != opFrame || string(p) != "hello" {
		t.Fatalf("got %d %q %v", op, p, err)
	}
}
