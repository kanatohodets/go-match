package queue

type Match struct {
	Game          string
	Map           string
	EngineVersion string
	Players       []*MatchedPlayer
}

type MatchedPlayer struct {
	Name string
	Team int
	Ally int
}
