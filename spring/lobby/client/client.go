package client

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"net"
	"time"
)

type Client struct {
	conn   net.Conn
	Events chan *protocol.Message
	exit   chan struct{}
}

func New() *Client {
	return &Client{
		Events: make(chan *protocol.Message),
		exit:   make(chan struct{}),
	}
}

func (c *Client) Connect(lobbyServer string) error {
	conn, err := net.Dial("tcp", lobbyServer)
	if err != nil {
		return err
	}

	c.conn = conn

	go c.read()

	return nil
}

func (c *Client) Disconnect() {
	c.send("EXIT", []string{})
	time.Sleep(1)
	c.conn.Close()
}

func (c *Client) Done() {
	<-c.exit
}

func (c *Client) Login(user string, pass string) {
	hash := md5.Sum([]byte(pass))

	params := []string{
		user,
		base64.StdEncoding.EncodeToString(hash[:]),
		"3200",
		"*",
		"Matchbot v0.1",
		"0",
		"sp cl p",
	}

	go c.keepAlive()

	c.send("LOGIN", params)
}

func (c *Client) OpenQueue(queueDef *protocol.QueueDefinition) {
	c.sendJSON("OPENQUEUE", queueDef)
}

func (c *Client) CloseQueue(queue string) {
	c.sendJSON("CLOSEQUEUE", &protocol.CloseQueue{Name: queue})
}

func (c *Client) JoinQueueAccept(queue string, users []string) {
	c.sendJSON("JOINQUEUEACCEPT", &protocol.JoinQueueAccept{
		Name:      queue,
		UserNames: users,
	})
}

func (c *Client) JoinQueueDeny(queue string, users []string, reason string) {
	c.sendJSON("JOINQUEUEDENY", &protocol.JoinQueueDeny{
		Name:      queue,
		UserNames: users,
		Reason:    reason,
	})
}

func (c *Client) ReadyCheck(queue string, users []string, responseTime int) {
	c.sendJSON("READYCHECK", &protocol.ReadyCheck{
		Name:         queue,
		UserNames:    users,
		ResponseTime: responseTime,
	})
}

func (c *Client) ReadyCheckResult(queue string, users []string, status string) {
	c.sendJSON("READYCHECKRESULT", &protocol.ReadyCheckResult{
		Name:      queue,
		UserNames: users,
		Result:    status,
	})
}

func (c *Client) ConnectUser(user string, ip string, port string, password string, engine string) {
	c.sendJSON("CONNECTUSER", &protocol.ConnectUser{
		UserName: user,
		IP:       ip,
		Port:     port,
		Password: password,
		Engine:   engine,
	})
}

func (c *Client) send(command string, params []string) {
	msg := protocol.Prepare(command, params)

	raw := msg.Bytes()
	log.WithFields(log.Fields{
		"event":  "send",
		"output": string(raw),
	}).Debug("OUT")

	_, err := c.conn.Write(raw)
	if err != nil {
		log.WithFields(log.Fields{
			"event":   "send",
			"command": command,
			"data":    params,
			"error":   err,
		}).Warn("error sending to spring server")
		c.conn.Close()
	}
}

func (c *Client) sendJSON(command string, payload interface{}) {
	b, err := json.Marshal(payload)
	if err != nil {
		log.WithFields(log.Fields{
			"event":   "sendJSON",
			"command": command,
			"payload": payload,
			"error":   err,
		}).Error("could not encode sendJSON payload")
		return
	}

	c.send(command, []string{string(b)})
}

func (c *Client) read() {
	scanner := bufio.NewScanner(c.conn)
	for scanner.Scan() {
		if err := scanner.Err(); err != nil {
			log.Printf("trouble reading from conn: %v", err)
			break
		}

		msg := protocol.Parse(scanner.Text())

		log.WithFields(log.Fields{
			"event":   "read",
			"command": msg.Command,
			"data":    string(msg.Data),
		}).Debug("IN")

		c.Events <- msg
	}

	close(c.Events)
	close(c.exit)
}

func (c *Client) keepAlive() {
	for {
		select {
		case <-c.exit:
			return

		case <-time.After(20 * time.Second):
			c.send("PING", nil)
		}
	}
}
