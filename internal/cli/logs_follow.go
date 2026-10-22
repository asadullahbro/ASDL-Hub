package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// jobBanner is the line the agent prints above and below a job's own output.
const jobBanner = "===================================="

// logPrinter prints streamed job output as whole lines, without the agent's header and footer. Chunks can end
// in the middle of a line, so the unfinished part is held until its end arrives.
type logPrinter struct {
	out    io.Writer
	rest   string
	inHead bool // still before the output proper: the header, which ends with a banner and a blank line
	seen   int  // chunks received while looking for the header's end
}

func newLogPrinter(out io.Writer) *logPrinter { return &logPrinter{out: out, inHead: true} }

// headerWait is how many chunks to wait for a header before deciding there is none and printing as it comes.
const headerWait = 3

func (p *logPrinter) write(chunk string) {
	p.rest += chunk
	if p.inHead {
		p.seen++
		if i := strings.Index(p.rest, jobBanner+"\n\n"); i >= 0 {
			p.rest, p.inHead = p.rest[i+len(jobBanner)+2:], false
		} else if p.seen >= headerWait {
			p.inHead = false
		} else {
			return
		}
	}
	for {
		i := strings.IndexByte(p.rest, '\n')
		if i < 0 {
			return
		}
		p.line(p.rest[:i])
		p.rest = p.rest[i+1:]
	}
}

// line prints one line, except the footer the agent adds when the job ends.
func (p *logPrinter) line(l string) {
	if strings.TrimSpace(l) == jobBanner || strings.HasPrefix(l, "Completed at:") {
		return
	}
	fmt.Fprintln(p.out, l)
}

// flush prints a last line that never got its newline.
func (p *logPrinter) flush() {
	if p.inHead {
		p.inHead = false
		p.write("")
	}
	if p.rest != "" {
		p.line(p.rest)
		p.rest = ""
	}
}

// streamURL is the websocket address of a job's live output.
func (c *client) streamURL(jobID string) (string, error) {
	u, err := url.Parse(strings.TrimRight(c.url, "/") + "/api/v1/jobs/" + url.PathEscape(jobID) + "/stream")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	return u.String(), nil
}

// followJob prints a job's output as it happens, until the job ends or the connection drops.
func (e *env) followJob(c *client, jobID string) error {
	addr, err := c.streamURL(jobID)
	if err != nil {
		return err
	}
	header := http.Header{"Authorization": {"Bearer " + c.token}, "User-Agent": {"asdl-hub-cli"}}
	ws, resp, err := (&websocket.Dialer{HandshakeTimeout: 15 * time.Second}).Dial(addr, header)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("the Hub refused the log stream (%s)", resp.Status)
		}
		return fmt.Errorf("can't open the log stream: %v", err)
	}
	defer ws.Close()

	p := newLogPrinter(e.out)
	defer p.flush()
	for {
		var msg struct {
			Type   string `json:"type"`
			Data   string `json:"data"`
			Status string `json:"status"`
		}
		if err := ws.ReadJSON(&msg); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return fmt.Errorf("the log stream ended unexpectedly: %v", err)
		}
		switch msg.Type {
		case "snapshot", "chunk":
			p.write(msg.Data)
		case "done":
			if msg.Status != "" && msg.Status != "completed" {
				return fmt.Errorf("log job %s", msg.Status)
			}
			return nil
		}
	}
}
