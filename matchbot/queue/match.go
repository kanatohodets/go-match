package queue

type Match struct {
	Id            uint64
	QueueName     string
	Game          string
	Map           string
	EngineVersion string
	Players       []*Player
}
