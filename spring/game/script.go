package game

type scriptAllyTeam struct {
	Id        int
	NumAllies int
}

type scriptPlayer struct {
	Id       int
	Name     string
	Password string
	Team     int
}

type scriptTeam struct {
	Id         int
	AllyTeam   int
	TeamLeader int
}

type startScript struct {
	IP           string
	Port         string
	Game         string
	Engine       string
	AutoHostPort string
	Map          string
	AllyTeams    map[int]*scriptAllyTeam
	Players      []*scriptPlayer
	Teams        map[int]*scriptTeam
}

var scriptTmpl string = `[game]
{
	AutoHostIP=127.0.0.1;
	AutoHostPort={{.AutoHostPort}};
	GameType={{.Game}};
	HostIP=;
	HostPort={{.Port}};
	IsHost=1;
	MapName={{.Map}};
	OnlyLocal=0;
	StartPosType=1;
	{{range .AllyTeams}}
	[allyteam{{.Id}}]
	{
		NumAllies={{.NumAllies}};
	}
	{{end}}
	{{range .Players}}
	[player{{.Id}}]
	{
		name={{.Name}};
		password={{.Password}};
		team={{.Team}};
	}
	{{end}}
	{{range .Teams}}
	[team{{.Id}}]
	{
		AllyTeam={{.AllyTeam}};
		TeamLeader={{.TeamLeader}};
	}
	{{end}}
}`
