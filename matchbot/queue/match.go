package queue

type Match struct {
	Queue         string
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
