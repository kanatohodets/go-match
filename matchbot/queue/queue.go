package queue

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/spring/lobby/protocol"
	"github.com/yuin/gopher-lua"
	"sync"
	"sync/atomic"
	"time"
)

type Queue struct {
	L    *lua.LState
	LMut sync.Mutex

	playersMut sync.Mutex
	players    map[string]*Player

	Def     *protocol.QueueDefinition
	Matches chan<- *Match

	matchId uint64
}

func NewQueue(def *protocol.QueueDefinition, matches chan<- *Match) (*Queue, error) {
	q := &Queue{
		L:       lua.NewState(),
		Def:     def,
		players: make(map[string]*Player),
		Matches: matches,

		matchId: 0, // yes, it defaults to zero, but TODO: read from KV store
	}

	q.populateAPI()
	// TODO: populate this from config/KV store
	file := "example/lua/bozo_1v1.lua"
	err := q.L.DoFile(file)
	if err != nil {
		return nil, fmt.Errorf("could not load %v: %v", file, err)
	}

	go q.luaUpdateCallin()
	return q, nil
}

func (q *Queue) populateAPI() {
	q.LMut.Lock()
	defer q.LMut.Unlock()

	queueNamespace := q.L.NewTable()
	// TODO: perhaps pull these out of here, get them access to q some other way
	q.L.SetFuncs(queueNamespace, map[string]lua.LGFunction{
		"GetTitle": func(L *lua.LState) int {
			q.L.Push(lua.LString(q.Def.Title))
			return 1
		},
		"GetPlayerList": func(L *lua.LState) int {
			tab := L.NewTable()
			q.playersMut.Lock()
			defer q.playersMut.Unlock()
			for name, player := range q.players {
				if player.Status() == Waiting {
					tab.Append(lua.LString(name))
				}
			}
			q.L.Push(tab)
			return 1
		},
		"GetMapList": func(L *lua.LState) int {
			tab := L.NewTable()
			for _, mapName := range q.Def.MapNames {
				tab.Append(lua.LString(mapName))
			}
			q.L.Push(tab)
			return 1
		},
		"GetGameList": func(L *lua.LState) int {
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

			q.playersMut.Lock()
			defer q.playersMut.Unlock()

			matchPlayers := []*Player{}
			errors := []string{}
			playersTable.ForEach(func(i lua.LValue, player lua.LValue) {

				// TODO validate
				name, ok := L.GetField(player, "name").(lua.LString)
				if !ok {
					errors = append(errors, fmt.Sprintf("player %d did not have a name defined", i))
					return
				}

				queuePlayer, ok := q.players[string(name)]
				if !ok {
					errors = append(errors, fmt.Sprintf("player %s is not in the queue!", name))
					return
				}

				if queuePlayer.Status() != Waiting {
					errors = append(errors, fmt.Sprintf("player %s is not status waiting!", name))
					return
				}

				team, ok := L.GetField(player, "team").(lua.LNumber)
				if !ok {
					errors = append(errors, fmt.Sprintf("player %s does not have a team!", name))
					return
				}

				allyTeam, ok := L.GetField(player, "ally").(lua.LNumber)
				if !ok {
					errors = append(errors, fmt.Sprintf("player %s does not have an allyteam!", name))
					return
				}

				queuePlayer.SetMatched(int(team), int(allyTeam))

				matchPlayers = append(matchPlayers, queuePlayer)
			})

			if len(errors) > 0 {
				log.Warn("bailing on this match, errors present, %v", errors)
				return 0
			}

			q.Matches <- &Match{
				Id:            q.newMatchId(),
				QueueName:     q.Def.Name,
				Map:           string(mapName),
				Game:          string(gameName),
				EngineVersion: string(engine),
				Players:       matchPlayers,
			}

			return 0
		},
	})

	q.L.SetGlobal("queue", queueNamespace)

}

// AddPlayer adds a player to the queue, triggering the queue.PlayerJoined Lua callback.
func (q *Queue) AddPlayer(name string) error {
	player := NewPlayer(name)

	q.LMut.Lock()
	defer q.LMut.Unlock()

	// protected by the LMut
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

	q.LMut.Lock()
	defer q.LMut.Unlock()

	// protected by the LMut
	delete(q.players, name)

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

func (q *Queue) luaUpdateCallin() {
	startTime := time.Now()
	for {
		q.LMut.Lock()
		callin, err := q.getLuaCallin("Update")
		if err != nil {
			log.Error("queue.RemovePlayer: cannot get lua callin %v: %v", "Update", err)
			q.LMut.Unlock()
			continue
		}

		elapsedSeconds := int(time.Since(startTime).Seconds())

		err = q.L.CallByParam(lua.P{
			Fn:      callin,
			NRet:    0,
			Protect: true,
		}, lua.LNumber(elapsedSeconds))

		q.LMut.Unlock()
		time.Sleep(1 * time.Second)
	}
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

func (q *Queue) newMatchId() uint64 {
	return atomic.AddUint64(&q.matchId, 1)
}
