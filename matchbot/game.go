package matchbot

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"github.com/kanatohodets/go-match/spring/game"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"time"
)

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

				g := game.New(match)

				err := g.Start()
				if err != nil {
					log.WithFields(log.Fields{
						"event":    "matchbot.readyCheckSpinner",
						"queue":    match.QueueName,
						"match_id": match.Id,
						"error":    err,
					}).Error("failure to start game!")

					m.client.ReadyCheckResult(
						match.QueueName,
						playerNames,
						"fail",
					)
					break Listen
				}

				log.WithFields(log.Fields{
					"event":    "matchbot.readyCheckSpinner",
					"queue":    match.QueueName,
					"match_id": match.Id,
					"players":  g.Script.Players,
				}).Info("game started, connecting players")

				for _, p := range g.Script.Players {
					m.client.ConnectUser(p.Name, g.Script.IP, g.Script.Port, p.Password, g.Script.Engine)
				}

				go m.manageGame(g)
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

func (m *Matchbot) manageGame(g *game.Game) {
	g.Wait()
	// TODO:
	/*
		//
		// send this off to nowhere -- we'll log it, but we don't actually use this
		// to decide when the game is over. for that, consume the autohost interface events
		go g.Wait()
		for event := range g.Events {
			switch event.Name {
			case "PlayerJoined":
				continue
			case "PlayerLeft":
				// etc., etc.
			}
		}
	*/
}
