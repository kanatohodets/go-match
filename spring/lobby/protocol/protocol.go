package protocol

import (
	"bytes"
	"strings"
)

type Message struct {
	Command string
	Data    []byte
}

func (m *Message) Bytes() []byte {
	buf := bytes.NewBufferString(m.Command)
	buf.WriteString(" ")
	buf.Write(m.Data)
	buf.WriteString("\n")
	return buf.Bytes()
}

// having the message data by a []byte causes some casting headaches here, but
// it makes life easy on the consuming side (since the payload can be given
// directly to json.Unmarshal)
func Prepare(command string, params []string) *Message {
	pieces := [][]byte{}
	for i, param := range params {
		// replace tabs with 2 spaces (tabs are a protocol-reserved character)
		if strings.Contains(param, "\t") {
			param = strings.Replace(param, "\t", "  ", -1)
		}

		sentence := strings.Contains(param, " ")
		if sentence {
			// force the previous seperator into tab
			if i > 0 {
				pieces[len(pieces)-1] = []byte("\t")
			}

			pieces = append(pieces, []byte(param))
			pieces = append(pieces, []byte("\t"))
		} else {
			pieces = append(pieces, []byte(param))
			pieces = append(pieces, []byte(" "))
		}
	}

	// chop off trailing whitespace
	if len(pieces) > 1 {
		pieces = pieces[:len(pieces)-1]
	}

	return &Message{
		Command: command,
		Data:    bytes.Join(pieces, []byte("")),
	}
}

func Parse(line string) *Message {
	mark := strings.Index(line, " ")
	// no space, no command params, so message is a single command like 'PONG'
	if mark == -1 {
		return &Message{
			Command: strings.TrimSpace(line),
			Data:    []byte(""),
		}
	}

	return &Message{
		Command: line[:mark],
		Data:    []byte(strings.TrimSpace(line[mark:])),
	}
}
