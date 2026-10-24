package services

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/asdl/hub/internal/models"
)

// maxLiveLog is how much of a running job's output the Hub keeps in memory for
// browsers that open the job late. The finished job's full logs still come
// from the database, so this only bounds the live view.
const maxLiveLog = 1 << 20

// finishedLinger keeps a finished job's live output around briefly so a
// browser that is mid-connect still gets the tail and the "done" message.
const finishedLinger = 30 * time.Second

// liveLog is one running job's output so far, and the browsers watching it.
type liveLog struct {
	buf    []byte // the most recent output, at most maxLiveLog bytes
	total  int    // bytes of output received overall (dropped ones included)
	done   bool
	status string
	subs   map[chan liveEvent]struct{}
}

type liveEvent struct {
	Chunk  string // new output; empty on the final event
	Done   bool
	Status string
}

// JobLogHub fans a running job's output out to the browsers watching it. It is
// in memory only: a Hub restart loses the live view, never the job's logs.
type JobLogHub struct {
	mu   sync.Mutex
	jobs map[string]*liveLog
}

func NewJobLogHub() *JobLogHub {
	return &JobLogHub{jobs: make(map[string]*liveLog)}
}

func (h *JobLogHub) get(id string) *liveLog {
	l := h.jobs[id]
	if l == nil {
		l = &liveLog{subs: make(map[chan liveEvent]struct{})}
		h.jobs[id] = l
	}
	return l
}

// Append adds output that starts at byte `offset` of the job's output. An agent
// that retries a request can resend what the Hub already has, so the part
// already seen is skipped.
func (h *JobLogHub) Append(id string, offset int, chunk string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.get(id)
	if l.done {
		return
	}
	if offset < l.total {
		skip := l.total - offset
		if skip >= len(chunk) {
			return
		}
		chunk = chunk[skip:]
	}
	l.total += len(chunk)
	l.buf = append(l.buf, chunk...)
	if len(l.buf) > maxLiveLog {
		l.buf = append([]byte(nil), l.buf[len(l.buf)-maxLiveLog:]...)
	}
	for ch := range l.subs {
		select {
		case ch <- liveEvent{Chunk: chunk}:
		default: // a stalled browser misses output rather than slowing the job
		}
	}
}

// Total is how many bytes of output the Hub has for the job, so an agent that
// lost contact can carry on from there.
func (h *JobLogHub) Total(id string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if l := h.jobs[id]; l != nil {
		return l.total
	}
	return 0
}

// Finish tells the watchers the job ended, then forgets its output a little later.
func (h *JobLogHub) Finish(id, status string) {
	h.mu.Lock()
	l := h.jobs[id]
	if l == nil {
		h.mu.Unlock()
		return
	}
	l.done, l.status = true, status
	for ch := range l.subs {
		select {
		case ch <- liveEvent{Done: true, Status: status}:
		default:
		}
	}
	h.mu.Unlock()

	time.AfterFunc(finishedLinger, func() {
		h.mu.Lock()
		delete(h.jobs, id)
		h.mu.Unlock()
	})
}

// Subscribe returns the output so far and a channel of what follows. If the
// job is already over, done is true and the channel is nil.
func (h *JobLogHub) Subscribe(id string) (snapshot string, done bool, status string, events chan liveEvent, cancel func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l := h.get(id)
	snapshot = string(l.buf)
	if l.done {
		return snapshot, true, l.status, nil, func() {}
	}
	ch := make(chan liveEvent, 256)
	l.subs[ch] = struct{}{}
	return snapshot, false, "", ch, func() {
		h.mu.Lock()
		delete(l.subs, ch)
		h.mu.Unlock()
	}
}

// ---- HTTP ----

var streamUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	// Same as the node terminal: only pages of the Hub itself
	CheckOrigin: SameOrigin,
}

// AppendLogs is the agent's side: output of a running job, as it happens.
func (s *JobService) AppendLogs(c *gin.Context) {
	var req struct {
		Offset int    `json:"offset"`
		Chunk  string `json:"chunk"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	var job models.Job
	if err := s.db.First(&job, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	// Only the node the job belongs to may write its output (identified by
	// source address, like Complete)
	node, err := nodeByAddress(s.db, c)
	if err != nil || node.ID != job.NodeID {
		c.JSON(http.StatusForbidden, gin.H{"error": "not authorized"})
		return
	}
	if job.Status != models.JobStatusRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "job is not running"})
		return
	}

	// An empty chunk is the agent asking whether this job is still wanted (the
	// 409 above is the answer); there is nothing to add.
	if req.Chunk != "" {
		s.logs.Append(job.ID, req.Offset, req.Chunk)
	}
	// Tell the agent how much we have, so it can resend a gap after an outage
	c.JSON(http.StatusOK, gin.H{"received": s.logs.Total(job.ID)})
}

// Stream sends a job's output to a browser: what exists now, then new output
// as it arrives, then a final "done" message. A job that already ended is
// answered from the database.
func (s *JobService) Stream(c *gin.Context) {
	var job models.Job
	if err := s.db.First(&job, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}

	ws, err := streamUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("job stream upgrade failed: %v", err)
		return
	}
	defer ws.Close()

	send := func(v gin.H) bool {
		ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return ws.WriteJSON(v) == nil
	}

	finished := job.Status != models.JobStatusPending && job.Status != models.JobStatusRunning
	if finished {
		send(gin.H{"type": "snapshot", "data": job.Logs})
		send(gin.H{"type": "done", "status": job.Status})
		return
	}

	snapshot, done, status, events, cancel := s.logs.Subscribe(job.ID)
	defer cancel()
	if !send(gin.H{"type": "snapshot", "data": snapshot}) {
		return
	}
	if done {
		send(gin.H{"type": "done", "status": status})
		return
	}

	// Detect the browser going away: reads fail once it closes
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-closed:
			return
		case <-ping.C:
			if !send(gin.H{"type": "ping"}) {
				return
			}
		case ev := <-events:
			if ev.Done {
				send(gin.H{"type": "done", "status": ev.Status})
				return
			}
			if !send(gin.H{"type": "chunk", "data": ev.Chunk}) {
				return
			}
		}
	}
}
