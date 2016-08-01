package matchbot

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"github.com/kanatohodets/go-match/spring/lobby/client"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"io/ioutil"
	"sync"
	"time"
)

// Matchbot represents the primary state of the matchmaker: queues, players, and a lobby client
type Matchbot struct {
	// protects against races from SIGINT and the reconnection loop with m.client
	mut *sync.Mutex

	client  *client.Client
	queues  map[string]*queue.Queue
	players map[string]*queue.Queue

	matches chan *queue.Match
}

// New gets you a fresh matchbot. only expected to be called once per program run.
func New() *Matchbot {
	return &Matchbot{
		mut:     &sync.Mutex{},
		queues:  make(map[string]*queue.Queue),
		players: make(map[string]*queue.Queue),

		matches: make(chan *queue.Match),
	}
}

// Start starts and maintains a matchbot's connection to the spring server
func (m *Matchbot) Start(server string, user string, password string, queuesFile string) {
	// infinite loop so there's a reconnect if the server shuts off
	for {
		m.mut.Lock()
		m.client = client.New()
		m.mut.Unlock()

		// this goroutine will exit when the client terminates
		go m.handleServerCommands()

		shutdown := make(chan struct{})
		go m.matchesToGames(shutdown)

		err := m.client.Connect(server)
		if err == nil {
			err = m.client.Login(user, password)
			if err != nil {
				log.WithFields(log.Fields{
					"event": "matchbot.Start",
					"error": err,
					"user":  user,
				}).Warn("client login connection error")
			}
			m.client.Done()
		} else {
			log.WithFields(log.Fields{
				"event": "matchbot.Start",
				"error": err,
			}).Info("client could not connect to spring server")
		}

		close(shutdown)

		m.mut.Lock()
		m.client = nil
		m.mut.Unlock()

		log.Info("something went wrong with the client: waiting 10 seconds and reconnecting")
		time.Sleep(10 * time.Second)
	}
}

// Shutdown cleanly terminates the matchbot, closing all hosted queues and gracefully exiting from the spring server
func (m *Matchbot) Shutdown() {
	m.mut.Lock()
	defer m.mut.Unlock()

	if m.client != nil {
		for name, _ := range m.queues {
			m.client.CloseQueue(name)
		}
		m.client.Disconnect()
	}
}

func (m *Matchbot) handleServerCommands() {
	for msg := range m.client.Events {
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
			log.Info("ready check response ", string(msg.Data))

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
	}

	var defs []*protocol.QueueDefinition
	err = json.Unmarshal(b, &defs)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "matchbot.openStaticQueues",
			"file":  queuesFile,
			"error": err,
		}).Fatal("could not decode queues file JSON")
	}

	for _, def := range defs {
		err := m.client.OpenQueue(def)
		if err != nil {
			log.WithFields(log.Fields{
				"event": "matchbot.openStaticQueues",
				"title": def.Title,
				"error": err,
			}).Error("client errored while opening queue. bad queue definition?")
		}
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

// TODO: ready-check-response cycle, followed by
// TODO: spring/game/game.go, which takes data here and spawns a server
func (m *Matchbot) matchesToGames(shutdown chan struct{}) {
	for {
		select {
		case match := <-m.matches:
			log.Printf("WHOOHOO MATCH map: %v game %v engine %v", match.Map, match.Game, match.EngineVersion)
			log.Info("how bout some players??")
			for _, player := range match.Players {
				log.Printf("name: %v, team: %v, ally %v", player.Name, player.Team, player.Ally)
			}
		case <-shutdown:
			return
		}
	}

}
