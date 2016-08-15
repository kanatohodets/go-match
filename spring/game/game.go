package game

import (
	"bufio"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/kanatohodets/go-match/matchbot/queue"
	"io"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"sync"
	"text/template"
	"time"
)

// Game represents a single running spring-dedicated server instance
type Game struct {
	// these could be structured to be passed in to each function, but
	// attaching them to the struct is very handy for reporting
	Match   *queue.Match
	GameDir string
	Script  *startScript

	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	shutdown chan struct{}
}

func New(match *queue.Match) *Game {
	return &Game{
		Match: match,
	}
}

func (g *Game) Shutdown() error {
	close(g.shutdown)
	return g.cmd.Process.Signal(os.Interrupt)
}

func (g *Game) Kill() error {
	close(g.shutdown)
	return g.cmd.Process.Kill()
}

func (g *Game) Start() error {
	err := g.prepareScript()
	if err != nil {
		return fmt.Errorf("game.Start: couldn't prepare startscript: %v", err)
	}

	spring, err := exec.LookPath("spring-dedicated")
	if err != nil {
		return fmt.Errorf("game.Start: couldn't find spring-dedicated: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("game.Start: couldn't get a path for the working dir: %v", err)
	}

	scriptFile := fmt.Sprintf("%s/%s/startscript.txt", wd, g.GameDir)

	cmd := &exec.Cmd{
		Path: spring,
		Args: []string{"", scriptFile},
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("game.Start: couldn't get stdout pipe: %v", err)
	}
	g.stdout = stdout

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("game.Start: couldn't get stderr pipe: %v", err)
	}
	g.stderr = stderr

	//TODO: set up the autohost interface listener

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("game.Start: couldn't start spring-dedicated: %v", err)
	}

	g.cmd = cmd

	return nil
}

func (g *Game) Wait() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	stdoutScanner := bufio.NewScanner(g.stdout)
	go func() {
		for stdoutScanner.Scan() {
			text := stdoutScanner.Text()
			log.WithFields(log.Fields{
				"event":    "spring",
				"queue":    g.Match.QueueName,
				"match_id": g.Match.Id,
				"text":     text,
			}).Debug("Spring stdout")
		}
		wg.Done()
	}()

	wg.Add(1)
	stderrScanner := bufio.NewScanner(g.stderr)
	go func() {
		for stderrScanner.Scan() {
			text := stderrScanner.Text()
			log.WithFields(log.Fields{
				"event":    "spring",
				"queue":    g.Match.QueueName,
				"match_id": g.Match.Id,
				"text":     text,
			}).Warn("Spring stderr")
		}
		wg.Done()
	}()

	wg.Wait()
	err := g.cmd.Wait()
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

func (g *Game) prepareScript() error {
	port, err := openPort()
	if err != nil {
		return err
	}

	hostPort, err := openPort()
	if err != nil {
		return err
	}

	script := &startScript{
		IP:           "127.0.0.1",
		Engine:       "103.0",
		Port:         port,
		Game:         g.Match.Game,
		AutoHostPort: hostPort,
		Map:          g.Match.Map,

		AllyTeams: map[int]*scriptAllyTeam{},
		Teams:     map[int]*scriptTeam{},
		Players:   make([]*scriptPlayer, len(g.Match.Players)),
	}

	for i, p := range g.Match.Players {
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
					"queue":            g.Match.QueueName,
					"match_id":         g.Match.Id,
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
	path := fmt.Sprintf("%s/%s/%d", prefix, g.Match.QueueName, time.Now().Unix())

	g.GameDir = path

	err = generateStartScript(path, script)
	if err != nil {
		return fmt.Errorf("game.PrepareScript: could not create startscript: %v", err)
	}

	g.Script = script

	return nil
}

func generateStartScript(path string, script *startScript) error {
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

// just a token so people can't spoof in a game. non-crypto rand is fine.
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
