package protocol

type QueueDefinition struct {
	MapNames        []string `json:"mapNames"`
	GameNames       []string `json:"gameNames"`
	Title           string   `json:"title"`
	MinPlayers      int      `json:"minPlayers"`
	EngineVersions  []string `json:"engineVersions"`
	Description     string   `json:"description"`
	MaxPlayers      int      `json:"maxPlayers"`
	Name            string   `json:"name"`
	TeamJoinAllowed bool     `json:"teamJoinAllowed"`
}
