package obsws

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn            *websocket.Conn
	password        string
	url             string
	pendingRequests map[string]chan requestResult
	mu              sync.Mutex
	connected       bool
	closed          chan struct{}
	readDone        chan struct{}
	reqSeq          uint64
}

type requestResult struct {
	payload json.RawMessage
	err     error
}

type helloMessage struct {
	Op int `json:"op"`
	D  struct {
		Authentication *struct {
			Challenge string `json:"challenge"`
			Salt      string `json:"salt"`
		} `json:"authentication"`
	} `json:"d"`
}

type identifiedMessage struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

type requestEnvelope struct {
	Op int `json:"op"`
	D  struct {
		RequestType string      `json:"requestType"`
		RequestID   string      `json:"requestId"`
		RequestData interface{} `json:"requestData,omitempty"`
	} `json:"d"`
}

type requestResponse struct {
	Op int `json:"op"`
	D  struct {
		RequestID     string          `json:"requestId"`
		RequestStatus requestStatus   `json:"requestStatus"`
		ResponseData  json.RawMessage `json:"responseData"`
	} `json:"d"`
}

type requestStatus struct {
	Result  bool   `json:"result"`
	Code    int    `json:"code"`
	Comment string `json:"comment"`
}

func NewClient(url, password string) *Client {
	return &Client{
		url:             url,
		password:        password,
		pendingRequests: make(map[string]chan requestResult),
		closed:          make(chan struct{}),
		readDone:        make(chan struct{}),
	}
}

func (c *Client) Connect() error {
	c.mu.Lock()
	if c.connected && c.conn != nil {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}

	var hello helloMessage
	if err := conn.ReadJSON(&hello); err != nil {
		_ = conn.Close()
		return err
	}
	if hello.Op != 0 {
		_ = conn.Close()
		return fmt.Errorf("unexpected hello op: %d", hello.Op)
	}

	identify := map[string]interface{}{
		"op": 1,
		"d": map[string]interface{}{
			"rpcVersion": 1,
		},
	}
	if hello.D.Authentication != nil {
		identifyD := identify["d"].(map[string]interface{})
		identifyD["authentication"] = computeAuthentication(c.password, hello.D.Authentication.Salt, hello.D.Authentication.Challenge)
	}

	if err := conn.WriteJSON(identify); err != nil {
		_ = conn.Close()
		return err
	}

	var identified identifiedMessage
	if err := conn.ReadJSON(&identified); err != nil {
		_ = conn.Close()
		return err
	}
	if identified.Op != 2 {
		_ = conn.Close()
		return fmt.Errorf("unexpected identify response op: %d", identified.Op)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.closed = make(chan struct{})
	c.readDone = make(chan struct{})
	c.pendingRequests = make(map[string]chan requestResult)
	c.mu.Unlock()

	go c.readLoop()
	return nil
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	if !c.connected && c.conn == nil {
		c.mu.Unlock()
		return
	}
	conn := c.conn
	closed := c.closed
	c.conn = nil
	c.connected = false
	if closed != nil {
		select {
		case <-closed:
		default:
			close(closed)
		}
	}
	for id, ch := range c.pendingRequests {
		delete(c.pendingRequests, id)
		ch <- requestResult{err: errors.New("obs client disconnected")}
		close(ch)
	}
	c.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}

func (c *Client) SendRequest(requestType string, data interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	conn := c.conn
	connected := c.connected
	if !connected || conn == nil {
		c.mu.Unlock()
		return nil, errors.New("obs websocket is not connected")
	}
	c.reqSeq++
	requestID := fmt.Sprintf("req-%d", c.reqSeq)
	ch := make(chan requestResult, 1)
	c.pendingRequests[requestID] = ch
	c.mu.Unlock()

	env := requestEnvelope{Op: 6}
	env.D.RequestType = requestType
	env.D.RequestID = requestID
	env.D.RequestData = data

	if err := conn.WriteJSON(env); err != nil {
		c.removePending(requestID)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	select {
	case result := <-ch:
		return result.payload, result.err
	case <-ctx.Done():
		c.removePending(requestID)
		return nil, ctx.Err()
	}
}

func (c *Client) StartReplayBuffer() error {
	_, err := c.SendRequest("StartReplayBuffer", nil)
	return err
}

func (c *Client) StopReplayBuffer() error {
	_, err := c.SendRequest("StopReplayBuffer", nil)
	return err
}

func (c *Client) SaveReplayBuffer() error {
	_, err := c.SendRequest("SaveReplayBuffer", nil)
	return err
}

func (c *Client) readLoop() {
	defer func() {
		c.mu.Lock()
		readDone := c.readDone
		c.mu.Unlock()
		if readDone != nil {
			close(readDone)
		}
		c.Disconnect()
	}()

	for {
		var raw map[string]json.RawMessage
		if err := c.conn.ReadJSON(&raw); err != nil {
			return
		}

		var op int
		if err := json.Unmarshal(raw["op"], &op); err != nil {
			continue
		}

		switch op {
		case 7:
			var resp requestResponse
			data, _ := json.Marshal(raw)
			if err := json.Unmarshal(data, &resp); err != nil {
				continue
			}
			result := requestResult{payload: resp.D.ResponseData}
			if !resp.D.RequestStatus.Result {
				result.err = fmt.Errorf("obs request failed (%d): %s", resp.D.RequestStatus.Code, resp.D.RequestStatus.Comment)
			}
			c.fulfillPending(resp.D.RequestID, result)
		case 5:
		default:
		}
	}
}

func (c *Client) fulfillPending(requestID string, result requestResult) {
	c.mu.Lock()
	ch, ok := c.pendingRequests[requestID]
	if ok {
		delete(c.pendingRequests, requestID)
	}
	c.mu.Unlock()
	if ok {
		ch <- result
		close(ch)
	}
}

func (c *Client) removePending(requestID string) {
	c.mu.Lock()
	ch, ok := c.pendingRequests[requestID]
	if ok {
		delete(c.pendingRequests, requestID)
	}
	c.mu.Unlock()
	if ok {
		close(ch)
	}
}

// computeAuthentication implements the obs-websocket v5 challenge-response
// authentication protocol as specified at:
// https://github.com/obsproject/obs-websocket/blob/master/docs/generated/protocol.md#authentication
//
// This is NOT password storage — the SHA-256 operations produce a one-time
// authentication token using a server-supplied salt and challenge nonce.
// The algorithm is mandated by the obs-websocket v5 protocol spec and cannot
// be changed on the client side.
func computeAuthentication(password, salt, challenge string) string {
	secret := sha256.Sum256([]byte(password + salt)) // lgtm[go/weak-sensitive-data-hashing]
	secretB64 := base64.StdEncoding.EncodeToString(secret[:])
	auth := sha256.Sum256([]byte(secretB64 + challenge)) // lgtm[go/weak-sensitive-data-hashing]
	return base64.StdEncoding.EncodeToString(auth[:])
}
