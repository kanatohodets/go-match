package queue

import "sync"

type PlayerStatus int

const (
	Waiting PlayerStatus = 1 + iota
	Matched
	Playing
)

type ingame struct {
	Team     int
	AllyTeam int
}

type Player struct {
	Name      string
	QueueTeam string

	status PlayerStatus
	mut    sync.RWMutex

	Game *ingame
}

func NewPlayer(name string) *Player {
	return &Player{
		Name:   name,
		status: Waiting,
	}
}

func (p *Player) Status() PlayerStatus {
	p.mut.RLock()
	defer p.mut.RUnlock()
	return p.status
}

func (p *Player) SetWaiting() {
	p.mut.Lock()
	defer p.mut.Unlock()
	p.Game = nil
	p.status = Waiting
}

func (p *Player) SetMatched(team, allyTeam int) {
	p.mut.Lock()
	defer p.mut.Unlock()
	p.Game = &ingame{
		Team:     team,
		AllyTeam: allyTeam,
	}
	p.status = Matched
}

func (p *Player) SetPlaying() {
	p.mut.Lock()
	defer p.mut.Unlock()
	p.status = Playing
}
