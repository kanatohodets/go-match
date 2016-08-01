package queue

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"github.com/yuin/gopher-lua"
)

type Queue struct {
	L       *lua.LState
	players map[string]*Player
	Def     *protocol.QueueDefinition
	Matches chan<- *Match
}

func NewQueue(def *protocol.QueueDefinition, matches chan<- *Match) (*Queue, error) {
	q := &Queue{
		L:       lua.NewState(),
		Def:     def,
		players: make(map[string]*Player),
		Matches: matches,
	}

	q.populateAPI()
	// TODO: populate this from config/KV store
	err := q.L.DoFile("example/lua/bozo_1v1.lua")
	return q, err
}

func (q *Queue) populateAPI() {
	queueNamespace := q.L.NewTable()
	// TODO: perhaps pull these out of here, get them access to q some other way
	q.L.SetFuncs(queueNamespace, map[string]lua.LGFunction{
		"Title": func(L *lua.LState) int {
			q.L.Push(lua.LString(q.Def.Title))
			return 1
		},
		"ListPlayers": func(L *lua.LState) int {
			tab := L.NewTable()
			for name, _ := range q.players {
				tab.Append(lua.LString(name))
			}
			q.L.Push(tab)
			return 1
		},
		"ListMaps": func(L *lua.LState) int {
			tab := L.NewTable()
			for _, mapName := range q.Def.MapNames {
				tab.Append(lua.LString(mapName))
			}
			q.L.Push(tab)
			return 1
		},
		"ListGames": func(L *lua.LState) int {
			tab := L.NewTable()
			for _, gameName := range q.Def.GameNames {
				tab.Append(lua.LString(gameName))
			}
			q.L.Push(tab)
			return 1
		},
		// TODO: un-uglify
		"NewMatch": func(L *lua.LState) int {
			match := L.ToTable(1)
			// TODO: validate better? deliver errors to the lua?
			mapName, ok := L.GetField(match, "map").(lua.LString)
			if !ok {
				log.Warn("omg bad")
				return 0
			}

			gameName, ok := L.GetField(match, "game").(lua.LString)
			if !ok {
				log.Warn("omg bad")
				return 0
			}

			//TODO: don't make the lua do this; it is probably only one version, and obvious
			engine, ok := L.GetField(match, "engineVersion").(lua.LString)
			if !ok {
				log.Warn("omg bad")
				return 0
			}

			playersTable, ok := L.GetField(match, "players").(*lua.LTable)
			if !ok {
				log.Warn("omg bad")
				return 0
			}

			players := []*MatchedPlayer{}
			playersTable.ForEach(func(_ lua.LValue, player lua.LValue) {
				// TODO validate
				name, ok := L.GetField(player, "name").(lua.LString)
				if !ok {
					log.Warn("omg bad")
					return
				}

				team, ok := L.GetField(player, "team").(lua.LNumber)
				if !ok {
					log.Warn("omg bad")
					return
				}

				allyTeam, ok := L.GetField(player, "ally").(lua.LNumber)
				if !ok {
					log.Warn("omg bad")
					return
				}

				players = append(players, &MatchedPlayer{
					Name: string(name),
					Team: int(team),
					Ally: int(allyTeam),
				})
			})

			q.Matches <- &Match{
				Queue:         q.Def.Name,
				Map:           string(mapName),
				Game:          string(gameName),
				EngineVersion: string(engine),
				Players:       players,
			}

			return 0
		},
	})

	q.L.SetGlobal("queue", queueNamespace)

}

// AddPlayer adds a player to the queue, triggering the queue.PlayerJoined Lua callback.
func (q *Queue) AddPlayer(name string) error {
	player := NewPlayer(name)
	q.players[name] = player

	callin, err := q.getLuaCallin("PlayerJoined")
	if err != nil {
		return fmt.Errorf("queue.AddPlayer: cannot get lua callin %v: %v", "PlayerJoined", err)
	}

	err = q.L.CallByParam(lua.P{
		Fn:      callin,
		NRet:    0,
		Protect: true,
	}, lua.LString(name))

	if err != nil {
		return fmt.Errorf("queue.AddPlayer: error calling 'PlayerJoined': %v", err)
	}

	return nil
}

// RemovePlayer drops a player from the queue. this happens on: user action, user client disconnect, or ready check failure. it triggers the queue.PlayerLeft Lua callback
func (q *Queue) RemovePlayer(name string) error {
	_, ok := q.players[name]
	if !ok {
		return fmt.Errorf("queue.RemovePlayer: asked to remove player who is not in the queue")
	}

	callin, err := q.getLuaCallin("PlayerLeft")
	if err != nil {
		return fmt.Errorf("queue.RemovePlayer: cannot get lua callin %v: %v", "PlayerLeft", err)
	}

	err = q.L.CallByParam(lua.P{
		Fn:      callin,
		NRet:    0,
		Protect: true,
	}, lua.LString(name))

	if err != nil {
		return fmt.Errorf("queue.RemovePlayer: error calling 'PlayerLeft': %v", err)
	}

	return nil
}

func (q *Queue) getLuaCallin(name string) (*lua.LFunction, error) {
	namespace := q.L.GetGlobal("queue")
	potentialCallin := q.L.GetField(namespace, name)

	callin, ok := potentialCallin.(*lua.LFunction)
	if !ok {
		return nil, fmt.Errorf("%v.%v is not a function", "queue", name)
	}

	return callin, nil
}
