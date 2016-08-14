package game

import (
	"bufio"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"github.com/kanatohodets/go-match/spring/lobby/client"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"text/template"
	"time"
)

// Game represents a single running spring-dedicated server instance
type Game struct {
	client *client.Client

	Match       *queue.Match
	StartScript *startScript

	shutdown chan struct{}
}

//TODO: proper structs, fill in the teams/allyteams/players correctly
func StartGame(client *client.Client, match *queue.Match) {
	port, err := openPort()
	if err != nil {
		log.WithFields(log.Fields{
			"event":    "game.StartGame",
			"queue":    match.QueueName,
			"match_id": match.Id,
			"error":    err,
		}).Error("could not get open port to host game, bailing out")
		//TODO tell players to go away
		return
	}

	hostPort, err := openPort()
	if err != nil {
		log.WithFields(log.Fields{
			"event":    "game.StartGame",
			"queue":    match.QueueName,
			"match_id": match.Id,
			"error":    err,
		}).Error("could not get open HostPort to communicate with game, bailing out")
		//TODO tell players to go away
		return
	}

	script := &startScript{
		IP:           "127.0.0.1",
		Engine:       "103.0",
		Port:         port,
		Game:         match.Game,
		AutoHostPort: hostPort,
		Map:          match.Map,

		AllyTeams: map[int]*scriptAllyTeam{},
		Teams:     map[int]*scriptTeam{},
		Players:   make([]*scriptPlayer, len(match.Players)),
	}

	for i, p := range match.Players {
		script.Players[i] = &scriptPlayer{
			Id:   i,
			Name: p.Name,
			// password only used to prevent player spoofing in game for this one match: not used by a human
			Password: generatePassword(),
			Team:     p.Game.Team,
		}

		t, ok := script.Teams[p.Game.Team]
		if ok {
			if t.AllyTeam != p.Game.AllyTeam {
				log.WithFields(log.Fields{
					"event":            "game.StartGame",
					"queue":            match.QueueName,
					"match_id":         match.Id,
					"team_id":          t.Id,
					"player_ally_team": p.Game.AllyTeam,
					"team_ally_team":   t.AllyTeam,
				}).Warn("player and team AllyTeams don't line up! ignoring and coercing player into team. This indicates a problem in the matchmaking Lua code")
			}
		} else {
			script.Teams[p.Game.Team] = &scriptTeam{
				Id:       p.Game.Team,
				AllyTeam: p.Game.AllyTeam,
				// we won't have AIs, so this field being populated by the first player's ID is fine.
				TeamLeader: i,
			}
		}

		_, ok = script.AllyTeams[p.Game.AllyTeam]
		if !ok {
			script.AllyTeams[p.Game.AllyTeam] = &scriptAllyTeam{
				Id:        p.Game.AllyTeam,
				NumAllies: 0,
			}
		}
	}

	prefix := "games"
	path := fmt.Sprintf("%s/%s/%d", prefix, match.QueueName, time.Now().Unix())
	err = generateStartScript(path, script, match)
	if err != nil {
		log.WithFields(log.Fields{
			"event":    "game.StartGame",
			"error":    err,
			"queue":    match.QueueName,
			"match_id": match.Id,
			"path":     path,
		}).Error("could not create startscript")
		//TODO tell players to go away
		return
	}

	spring, err := exec.LookPath("spring-dedicated")
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("spring-dedicated isn't in the $PATH!")
		//TODO tell players to go away
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("couldn't get the working directory!")
		//TODO tell players to go away
		return
	}

	scriptFile := fmt.Sprintf("%s/%s/startscript.txt", wd, path)

	cmd := &exec.Cmd{
		Path: spring,
		Args: []string{"", scriptFile},
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("could not attach stdout pipe to spring-dedicated")
		//TODO tell players to go away
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("could not attach stderr pipe to spring-dedicated")
		//TODO tell players to go away
		return
	}

	err = cmd.Start()
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("spring-dedicated could not start!")
		//TODO tell players to go away
	}

	stdoutScanner := bufio.NewScanner(stdout)
	go func() {
		for stdoutScanner.Scan() {
			text := stdoutScanner.Text()
			log.WithFields(log.Fields{
				"event":    "spring",
				"queue":    match.QueueName,
				"match_id": match.Id,
				"text":     text,
			}).Debug("Spring stdout")
		}
	}()

	stderrScanner := bufio.NewScanner(stderr)
	go func() {
		for stderrScanner.Scan() {
			text := stderrScanner.Text()
			log.WithFields(log.Fields{
				"event":    "spring",
				"queue":    match.QueueName,
				"match_id": match.Id,
				"text":     text,
			}).Warn("Spring stderr")
		}
	}()

	for _, p := range script.Players {
		client.ConnectUser(p.Name, script.IP, script.Port, p.Password, script.Engine)
	}

	//TODO: set up the autohost interface listener
	err = cmd.Wait()
	if err != nil {
		log.WithFields(log.Fields{
			"event": "game.StartGame",
			"error": err,
		}).Error("spring-dedicated exited with an error")
	}

	//TODO: game over, put players back in queue?
	log.WithFields(log.Fields{
		"event": "game.StartGame",
	}).Info("game all done@!!!")
}

func generateStartScript(path string, script *startScript, match *queue.Match) error {
	tmpl, err := template.New("startScript").Parse(scriptTmpl)
	if err != nil {
		return fmt.Errorf("failed to compile template: %v", err)
	}

	//TODO: configurable
	err = os.MkdirAll(path, 0744)
	if err != nil {
		return fmt.Errorf("could not mkdir %v", err)
	}

	f, err := os.Create(fmt.Sprintf("%s/startscript.txt", path))
	if err != nil {
		return fmt.Errorf("could not create startscript.txt: %v", err)
	}
	defer f.Close()

	err = tmpl.Execute(f, script)
	if err != nil {
		return fmt.Errorf("failed to execute template: %v", err)
	}
	return nil
}

var alphabet string = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!$%^&*()"

func generatePassword() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	password := make([]byte, 50)
	for i, _ := range password {
		password[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(password)
}

func openPort() (string, error) {
	addr, err := net.ResolveUDPAddr("udp", "localhost:0")
	if err != nil {
		return "", fmt.Errorf("game.openPort: could not ResolveUDPAddr: %v", err)
	}

	l, err := net.ListenUDP("udp", addr)
	if err != nil {
		return "", fmt.Errorf("game.openPort: could not listenUDP: %v", err)
	}
	defer l.Close()

	_, port, err := net.SplitHostPort(l.LocalAddr().String())
	if err != nil {
		return "", fmt.Errorf("game.openPort: could not split addr: %v", err)
	}

	return port, nil
}
