package client

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func (c *Client) Login(user string, pass string) error {
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

	return c.send("LOGIN", params)
}

func (c *Client) OpenQueue(queueDef *protocol.QueueDefinition) error {
	return c.sendJSON("OPENQUEUE", queueDef)
}

func (c *Client) CloseQueue(queue string) error {
	return c.sendJSON("CLOSEQUEUE", &protocol.CloseQueue{Name: queue})
}

func (c *Client) JoinQueueAccept(queue string, users []string) error {
	return c.sendJSON("JOINQUEUEACCEPT", &protocol.JoinQueueAccept{
		Name:      queue,
		UserNames: users,
	})
}

func (c *Client) JoinQueueDeny(queue string, users []string, reason string) error {
	return c.sendJSON("JOINQUEUEDENY", &protocol.JoinQueueDeny{
		Name:      queue,
		UserNames: users,
		Reason:    reason,
	})
}

func (c *Client) send(command string, params []string) error {
	msg := protocol.Prepare(command, params)

	raw := msg.Bytes()
	log.WithFields(log.Fields{
		"event":  "send",
		"output": string(raw),
	}).Debug("OUT")

	_, err := c.conn.Write(raw)
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("could not send %v:%v to spring server: %v", command, params, err)
	}

	return nil
}

func (c *Client) sendJSON(command string, payload interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error encoding payload into JSON: %v", err)
	}

	return c.send(command, []string{string(b)})
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
