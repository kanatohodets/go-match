package queue

type PlayerStatus int

const (
	Waiting PlayerStatus = 1 + iota
	Matched
	Playing
)

type Player struct {
	Name   string
	Status PlayerStatus
}

func NewPlayer(name string) *Player {
	return &Player{
		Name:   name,
		Status: Waiting,
	}
}
