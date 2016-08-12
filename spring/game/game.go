package game

import (
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"os"
	"text/template"
)

var scriptTmpl string = `
[game]
{
	AutoHostIP=127.0.0.1;
	AutoHostPort={{.Port}};
	GameType={{.Game}};
	HostIP=;
	HostPort={{.HostPort}};
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
}
`

func StartGame(match *queue.Match) {
	tmpl, err := template.New("startScript").Parse(scriptTmpl)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("failed to compile template")
		return
	}

	//TODO: proper structs, fill in the teams/allyteams/players correctly
	type allyTeam struct {
		Id        int
		NumAllies int
	}

	type player struct {
		Id       int
		Name     string
		Password string
		Team     int
	}

	type team struct {
		Id         int
		AllyTeam   int
		TeamLeader int
	}

	type game struct {
		Port      string
		Game      string
		HostPort  string
		Map       string
		AllyTeams []*allyTeam
		Players   []*player
		Teams     []*team
	}

	d := &game{
		Port:     "61060",
		Game:     match.Game,
		HostPort: "60662",
		Map:      match.Map,
		AllyTeams: []*allyTeam{
			&allyTeam{
				Id:        0,
				NumAllies: 0,
			},
			&allyTeam{
				Id:        1,
				NumAllies: 0,
			},
		},
		Players: []*player{
			&player{
				Id:       0,
				Name:     match.Players[0].Name,
				Password: "god",
				Team:     match.Players[0].Team,
			},
			&player{
				Id:       1,
				Name:     match.Players[1].Name,
				Password: "god",
				Team:     match.Players[1].Team,
			},
		},
		Teams: []*team{
			&team{
				Id:         0,
				AllyTeam:   0,
				TeamLeader: 0,
			},
			&team{
				Id:         1,
				AllyTeam:   1,
				TeamLeader: 1,
			},
		},
	}

	//TODO: make a directory for this script to live in, write output there instead of to stdout
	err = tmpl.Execute(os.Stdout, d)
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("failed to execute template")
		return
	}
	//TODO: start the spring server
}
