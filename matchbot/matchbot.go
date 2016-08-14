package matchbot

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"github.com/kanatohodets/go-match/spring/game"
	"github.com/kanatohodets/go-match/spring/lobby/client"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"io/ioutil"
	"sync"
	"time"
)

// Matchbot represents the primary state of the matchmaker: queues, players, and a lobby client
type Matchbot struct {
	// protects against races from SIGINT and the reconnection loop with m.client
	client *client.Client

	shutdown chan struct{}

	queueMut sync.Mutex
	queues   map[string]*queue.Queue
	players  map[string]*queue.Queue

	matches chan *queue.Match

	readyMut sync.RWMutex
	// this will be a bug if there are > 4294967295 concurrent matches in the 'readyCheckSpinner' state.
	readyID uint32
	ready   map[uint32]chan *protocol.ReadyCheckResponse
}

// New gets you a fresh matchbot. only expected to be called once per program run.
func New() *Matchbot {
	return &Matchbot{
		queues:  make(map[string]*queue.Queue),
		players: make(map[string]*queue.Queue),

		matches:  make(chan *queue.Match),
		shutdown: make(chan struct{}),

		client: client.New(),

		ready: make(map[uint32]chan *protocol.ReadyCheckResponse),
	}
}

// Start starts and maintains a matchbot's connection to the spring server
func (m *Matchbot) Start(server string, user string, password string, queuesFile string) {
	// this goroutine will exit when m.shutdown is closed
	go m.matchesToGames()

	// infinite loop so there's a reconnect if the server shuts off
	for {
		err := m.client.Connect(server)
		if err == nil {
			// this goroutine will exit when the client terminates
			go m.handleServerCommands(m.client.Events)
			m.client.Login(user, password)

			// blocks until the client terminates
			m.client.Done()
			log.Info("client closed connection")
		} else {
			log.WithFields(log.Fields{
				"event": "matchbot.Start",
				"error": err,
			}).Info("client could not connect to spring server")
		}

		time.Sleep(10 * time.Second)
	}
}

// Shutdown cleanly terminates the matchbot, closing all hosted queues and gracefully exiting from the spring server
func (m *Matchbot) Shutdown() {
	close(m.shutdown)
	if m.client.Active() {
		m.queueMut.Lock()
		for name, _ := range m.queues {
			m.client.CloseQueue(name)
		}
		m.queueMut.Unlock()

		m.client.Disconnect()
	}
}

func (m *Matchbot) handleServerCommands(events chan *protocol.Message) {
	for msg := range events {
		switch msg.Command {

		// commands we don't care about
		case "TASServer":
			continue
		case "MOTD":
			continue
		case "PONG":
			continue
		case "ADDUSER":
			continue
		case "CLIENTSTATUS":
			continue
		case "OPENQUEUE":
			continue
		case "ACCEPTED":
			continue

		case "LOGININFOEND":
			//TODO(btyler) config-ify filename; save in KV store, set in web interface???
			m.openStaticQueues("example/queue.json")

		// matchmaking commands
		case "JOINQUEUEREQUEST":
			m.addPlayers(msg.Data)
		case "QUEUELEFT":
			m.removePlayers(msg.Data)
		case "REMOVEUSER":
			player := string(msg.Data)
			queue, ok := m.players[player]
			if ok {
				queue.RemovePlayer(player)
			}
		case "READYCHECKRESPONSE":
			m.readyCheckResponse(msg.Data)

		// TODO open earlier to avoid racing clients who try to join?
		case "QUEUEOPENED":
			m.addQueue(msg.Data)

		// chit chat
		case "SERVERMSG":
			log.WithFields(log.Fields{
				"event":   "matchbot.handleServerCommands",
				"command": msg.Command,
				"data":    string(msg.Data),
			}).Info("server message")

		case "FAILED":
			log.WithFields(log.Fields{
				"event":   "matchbot.handleServerCommands",
				"command": msg.Command,
				"data":    string(msg.Data),
			}).Error("failed command")

		// confusing commands
		default:
			log.WithFields(log.Fields{
				"event":   "matchbot.handleServerCommands",
				"command": msg.Command,
				"data":    string(msg.Data),
			}).Warn("unknown server command")
		}
	}
}

func (m *Matchbot) addPlayers(raw []byte) {
	var msg protocol.JoinQueueRequest
	err := json.Unmarshal(raw, &msg)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.addPlayers",
			"data":  string(raw),
			"error": err,
		}).Error("could not unmarshal join queue request")

		m.client.JoinQueueDeny(
			msg.Name,
			msg.UserNames,
			fmt.Sprintf("matchbot choked on JOINQUEUEREQUEST from server. contact an admin! error: %v", err),
		)
		return
	}

	queue, ok := m.queues[msg.Name]
	if !ok {
		log.WithFields(log.Fields{
			"event":     "matchbot.addPlayer",
			"userNames": msg.UserNames,
			"queueName": msg.Name,
		}).Error("got a JOIN QUEUE request for a queue I don't know about")

		m.client.JoinQueueDeny(
			msg.Name,
			msg.UserNames,
			fmt.Sprintf("matchbot does not know about queue %v: something went wrong, contact the admin!", msg.Name),
		)
		return
	}

	doubleMatchers := map[string][]string{}
	errored := map[error][]string{}
	successful := []string{}

	for _, player := range msg.UserNames {
		current, ok := m.players[player]
		if ok {
			// build up a list of players who are already in a queue
			doubleMatchers[current.Def.Name] = append(doubleMatchers[current.Def.Name], player)
			continue
		}

		err := queue.AddPlayer(player)
		if err != nil {
			log.WithFields(log.Fields{
				"event": "matchbot.addPlayer",
				"user":  player,
				"queue": queue.Def.Name,
				"error": err,
			}).Warn("could not add player to queue")

			errored[err] = append(errored[err], player)
			continue
		}

		m.players[player] = queue
		successful = append(successful, player)
	}

	for queue, players := range doubleMatchers {
		m.client.JoinQueueDeny(
			msg.Name,
			players,
			fmt.Sprintf("already waiting in %v. Leave that queue before joining another!", queue),
		)
	}

	for err, players := range errored {
		m.client.JoinQueueDeny(
			msg.Name,
			players,
			fmt.Sprintf("matchbot error adding to queue! ask admin to check logs. error: %v", err),
		)
	}

	if len(successful) > 0 {
		m.client.JoinQueueAccept(
			msg.Name,
			successful,
		)
	}
}

func (m *Matchbot) removePlayers(raw []byte) {
	var msg protocol.QueueLeft
	err := json.Unmarshal(raw, &msg)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.removePlayers",
			"data":  string(raw),
			"error": err,
		}).Error("could not unmarshal remove queue request")

		return
	}

	queue, ok := m.queues[msg.Name]
	if !ok {
		log.WithFields(log.Fields{
			"event":     "matchbot.removePlayers",
			"userNames": msg.UserNames,
			"queueName": msg.Name,
		}).Error("leave queue request for unknown queue")
		return
	}

	for _, player := range msg.UserNames {
		playerQueue, ok := m.players[player]
		if !ok {
			log.WithFields(log.Fields{
				"event":     "matchbot.removePlayers",
				"user":      player,
				"queueName": msg.Name,
			}).Error("player asked to leave a queue, but we don't think the player is IN a queue! blarg!")
			continue
		}

		if playerQueue != queue {
			log.WithFields(log.Fields{
				"event":          "matchbot.removePlayers",
				"user":           player,
				"playerQueue":    playerQueue.Def.Name,
				"requestedQueue": msg.Name,
			}).Error("player asked to leave a different queue from the one we think she's in. bad!")

			// this is an error, but we should respect the "get me out of this
			// queue" wish. particularly since a player will be disallowed from
			// joining another queue while still a part of this one.
			err := playerQueue.RemovePlayer(player)
			if err != nil {
				log.WithFields(log.Fields{
					"event": "matchbot.removePlayers",
					"user":  player,
					"queue": playerQueue.Def.Name,
					"error": err,
				}).Error("error while removing player from 'wrong' queue (mismatch case)")
			}
			// no continue here since it does no significant harm (one more
			// error) to attempt to remove the player from the requested queue,
			// plus we'd like to delete them from the 'players' map in either
			// case.
		}

		err := queue.RemovePlayer(player)
		if err != nil {
			log.WithFields(log.Fields{
				"event": "matchbot.removePlayers",
				"user":  player,
				"queue": queue.Def.Name,
				"error": err,
			}).Error("error while removing player from queue")
		}

		delete(m.players, player)
	}
}

func (m *Matchbot) openStaticQueues(queuesFile string) {
	b, err := ioutil.ReadFile(queuesFile)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.openStaticQueues",
			"file":  queuesFile,
			"error": err,
		}).Fatal("could not read static queues file")
		return
	}

	var defs []*protocol.QueueDefinition
	err = json.Unmarshal(b, &defs)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.openStaticQueues",
			"file":  queuesFile,
			"error": err,
		}).Fatal("could not decode queues file JSON")
		return
	}

	for _, def := range defs {
		m.client.OpenQueue(def)
	}
}

func (m *Matchbot) addQueue(raw []byte) {
	var def protocol.QueueDefinition
	err := json.Unmarshal(raw, &def)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.addQueue",
			"error": err,
			"data":  string(raw),
		}).Warn("couldn't unmarshal new queue data")
		return
	}

	q, err := queue.NewQueue(&def, m.matches)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.addQueue",
			"error": err,
			"data":  string(raw),
		}).Warn("failed to instantiate new queue")
		return
	}

	m.queues[def.Name] = q
}

func (m *Matchbot) readyCheckResponse(raw []byte) {
	var res protocol.ReadyCheckResponse
	err := json.Unmarshal(raw, &res)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.readyCheckResponse",
			"data":  string(raw),
			"error": err,
		}).Error("invalid READYCHECKRESPONSE from server")
		return
	}

	m.readyMut.RLock()
	// broadcast to all readyCheckSpinners
	for _, ch := range m.ready {
		ch <- &res
	}
	m.readyMut.RUnlock()
}

func (m *Matchbot) matchesToGames() {
	for {
		select {
		case match := <-m.matches:
			playerNames := make([]string, len(match.Players))
			for i, player := range match.Players {
				playerNames[i] = player.Name
			}
			// TODO: configurable timeout for user ready check
			m.client.ReadyCheck(match.QueueName, playerNames, 10)

			// Spawn a goroutine to represent this match.
			m.readyMut.Lock()
			m.readyID++
			ch := make(chan *protocol.ReadyCheckResponse)
			m.ready[m.readyID] = ch
			// The goroutine will exit when m.shutdown is closed.
			go m.readyCheckSpinner(m.readyID, match, ch)
			m.readyMut.Unlock()

		case <-m.shutdown:
			return
		}
	}
}

func (m *Matchbot) readyCheckSpinner(id uint32, match *queue.Match, ch chan *protocol.ReadyCheckResponse) {
	playerNames := make([]string, len(match.Players))
	playerReadyStatus := make(map[string]bool)
	requiredReady := len(match.Players)
	seenReady := 0
	for i, player := range match.Players {
		playerNames[i] = player.Name
		playerReadyStatus[player.Name] = false
	}

	log.WithFields(log.Fields{
		"event":    "matchbot.readyCheckSpinner",
		"queue":    match.QueueName,
		"match_id": match.Id,
		"players":  playerNames,
	}).Info("Entering readyCheck spinner")

Listen:
	for {
		select {
		case readyCheck := <-ch:
			if readyCheck.Name != match.QueueName {
				continue
			}

			readied, ok := playerReadyStatus[readyCheck.UserName]
			if !ok {
				log.Info("got a ready for a player we don't care about")
				continue
			}

			log.WithFields(log.Fields{
				"event":           "matchbot.readyCheckSpinner",
				"player":          readyCheck.UserName,
				"status":          readyCheck.Response,
				"queue":           match.QueueName,
				"match_id":        match.Id,
				"already_readied": readied,
			}).Debug("got a readycheck response")

			if readyCheck.Response != "ready" {
				m.client.ReadyCheckResult(
					match.QueueName,
					playerNames,
					fmt.Sprintf("%s responded with status %s", readyCheck.UserName, readyCheck.Response),
				)

				break Listen
			}

			// protect against a single player sending 'ready' many times
			if !readied {
				seenReady++
				playerReadyStatus[readyCheck.UserName] = true
			}

			if seenReady == requiredReady {
				log.WithFields(log.Fields{
					"event":    "matchbot.readyCheckSpinner",
					"queue":    match.QueueName,
					"match_id": match.Id,
					"players":  playerNames,
				}).Info("ready check complete, starting game")

				m.client.ReadyCheckResult(
					match.QueueName,
					playerNames,
					"pass",
				)

				go game.StartGame(m.client, match)
				break Listen
			}

		case <-m.shutdown:
			break Listen
		case <-time.After(10 * time.Second):
			log.Info("a ready check timed out")
			m.client.ReadyCheckResult(
				match.QueueName,
				playerNames,
				"timeout waiting for players to ready up",
			)

			break Listen
		}
	}

	m.readyMut.Lock()
	delete(m.ready, id)
	m.readyMut.Unlock()
}
