package services

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asdl/hub/internal/models"
	"github.com/asdl/hub/internal/testutil"
)

type captured struct {
	path   string
	header http.Header
	body   []byte
}

// captureServer records every request it gets.
func captureServer(t *testing.T) (*httptest.Server, func() []captured) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, captured{r.URL.Path, r.Header.Clone(), b})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []captured {
		mu.Lock()
		defer mu.Unlock()
		return append([]captured(nil), got...)
	}
}

var testEvent = Event{
	Type: EventDeployFailed, Level: LevelError,
	Title: "api failed to deploy", Message: "Deploying api on pc failed.",
	Fields:  []EventField{{Name: "Node", Value: "pc"}},
	Details: "pull access denied",
	URL:     "https://hub.example.com/jobs?id=1",
	Time:    time.Unix(1700000000, 0),
}

func TestSenders_Payloads(t *testing.T) {
	srv, got := captureServer(t)
	n := &notifier{client: srv.Client(), hubURL: "https://hub.example.com"}
	ctx := context.Background()

	if err := sendDiscord(ctx, n, map[string]string{"webhook_url": srv.URL + "/discord", "mention": "@here"}, testEvent); err != nil {
		t.Fatal(err)
	}
	if err := sendSlack(ctx, n, map[string]string{"webhook_url": srv.URL + "/slack"}, testEvent); err != nil {
		t.Fatal(err)
	}
	old := telegramAPI
	telegramAPI = srv.URL
	defer func() { telegramAPI = old }()
	if err := sendTelegram(ctx, n, map[string]string{"bot_token": "123:abc", "chat_id": "42"}, testEvent); err != nil {
		t.Fatal(err)
	}
	if err := sendNtfy(ctx, n, map[string]string{"server": srv.URL, "topic": "asdl-test", "token": "tk"}, testEvent); err != nil {
		t.Fatal(err)
	}
	if err := sendWebhook(ctx, n, map[string]string{"url": srv.URL + "/hook", "secret": "s3"}, testEvent); err != nil {
		t.Fatal(err)
	}

	reqs := got()
	if len(reqs) != 5 {
		t.Fatalf("got %d requests, want 5", len(reqs))
	}

	var discord struct {
		Content string `json:"content"`
		Embeds  []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Color       int    `json:"color"`
			URL         string `json:"url"`
		} `json:"embeds"`
		AvatarURL string `json:"avatar_url"`
	}
	json.Unmarshal(reqs[0].body, &discord)
	if len(discord.Embeds) != 1 || discord.Embeds[0].Title != testEvent.Title || discord.Embeds[0].Color != 0xef4444 ||
		!strings.Contains(discord.Embeds[0].Description, "pull access denied") || discord.Content != "@here" ||
		discord.AvatarURL != "https://hub.example.com/apple-icon.png" {
		t.Errorf("discord payload: %s", reqs[0].body)
	}

	var slack struct {
		Text        string `json:"text"`
		Attachments []struct {
			Color     string `json:"color"`
			TitleLink string `json:"title_link"`
		} `json:"attachments"`
	}
	json.Unmarshal(reqs[1].body, &slack)
	if !strings.Contains(slack.Text, testEvent.Title) || len(slack.Attachments) != 1 || slack.Attachments[0].Color != "#ef4444" ||
		slack.Attachments[0].TitleLink != testEvent.URL {
		t.Errorf("slack payload: %s", reqs[1].body)
	}

	if reqs[2].path != "/bot123:abc/sendMessage" {
		t.Errorf("telegram path %s", reqs[2].path)
	}
	var tg map[string]interface{}
	json.Unmarshal(reqs[2].body, &tg)
	if tg["chat_id"] != "42" || tg["parse_mode"] != "HTML" || !strings.Contains(tg["text"].(string), "<b>api failed to deploy</b>") {
		t.Errorf("telegram payload: %s", reqs[2].body)
	}

	if reqs[3].path != "/asdl-test" || reqs[3].header.Get("Priority") != "5" || reqs[3].header.Get("Title") != testEvent.Title ||
		reqs[3].header.Get("Authorization") != "Bearer tk" || reqs[3].header.Get("Click") != testEvent.URL {
		t.Errorf("ntfy request: %s %v", reqs[3].path, reqs[3].header)
	}

	if sig := reqs[4].header.Get("X-ASDL-Signature"); sig != "sha256="+signBody("s3", reqs[4].body) {
		t.Errorf("webhook signature %q", sig)
	}
	var ev Event
	if err := json.Unmarshal(reqs[4].body, &ev); err != nil || ev.Type != EventDeployFailed || ev.Title != testEvent.Title {
		t.Errorf("webhook body: %s", reqs[4].body)
	}
}

func TestTelegram_ErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"ok":false,"description":"Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	old := telegramAPI
	telegramAPI = srv.URL
	defer func() { telegramAPI = old }()
	err := sendTelegram(context.Background(), &notifier{client: srv.Client()}, map[string]string{"bot_token": "999:secret", "chat_id": "1"}, testEvent)
	if err == nil || strings.Contains(err.Error(), "999:secret") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

// fakeSMTP accepts one message without TLS or auth and returns it.
func fakeSMTP(t *testing.T) (string, chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { io.WriteString(c, s+"\r\n") }
		say("220 fake")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					out <- data.String()
					say("250 queued")
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case cmd == "DATA":
				inData = true
				say("354 go")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().String(), out
}

func TestEmail_SendsMultipartMessage(t *testing.T) {
	addr, out := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	cfg := map[string]string{"host": host, "port": port, "from": "ASDL Hub <hub@example.com>", "to": "a@example.com, b@example.com"}
	if err := sendEmail(context.Background(), &notifier{}, cfg, testEvent); err != nil {
		t.Fatal(err)
	}
	msg := <-out
	for _, want := range []string{"Subject: [ASDL Hub] api failed to deploy", "To: <a@example.com>, <b@example.com>",
		"text/html", "pull access denied", "https://hub.example.com/jobs?id=1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}

func TestApply_ValidatesAndKeepsMaskedSecrets(t *testing.T) {
	s := &NotificationService{db: testutil.NewDB(t, &models.Setting{})}
	ch := models.NotificationChannel{Type: "discord"}
	if err := s.apply(&ch, channelRequest{Config: []models.EnvVar{}}); err == nil {
		t.Fatal("missing webhook URL accepted")
	}
	if err := s.apply(&ch, channelRequest{Config: []models.EnvVar{{Key: "webhook_url", Value: "https://evil.example.com/x"}}}); err == nil {
		t.Fatal("non-Discord URL accepted")
	}
	url := "https://discord.com/api/webhooks/1/abc"
	if err := s.apply(&ch, channelRequest{Config: []models.EnvVar{{Key: "webhook_url", Value: url}}, Events: []string{EventDeployFailed}}); err != nil {
		t.Fatal(err)
	}
	if ch.Name != "Discord" {
		t.Errorf("default name %q", ch.Name)
	}
	// The dashboard sends back the masked value it was given.
	if err := s.apply(&ch, channelRequest{Name: "Ops", Config: []models.EnvVar{{Key: "webhook_url", Value: models.MaskedValue}, {Key: "mention", Value: "@here"}}}); err != nil {
		t.Fatal(err)
	}
	if got := configMap(ch.Config); got["webhook_url"] != url || got["mention"] != "@here" || ch.Name != "Ops" {
		t.Errorf("config %v name %q", got, ch.Name)
	}
	if err := s.apply(&ch, channelRequest{Events: []string{"nope"}}); err == nil {
		t.Error("unknown event accepted")
	}

	v := viewChannel(ch, s.typeByID("discord"))
	if c := configMap(v.Config); c["webhook_url"] != models.MaskedValue || c["mention"] != "@here" {
		t.Errorf("view config %v", c)
	}

	ntfy := models.NotificationChannel{Type: "ntfy"}
	if err := s.apply(&ntfy, channelRequest{Config: []models.EnvVar{{Key: "topic", Value: "abc"}}}); err != nil {
		t.Fatal(err)
	}
	if configMap(ntfy.Config)["server"] != "https://ntfy.sh" {
		t.Errorf("ntfy default server not set: %v", ntfy.Config)
	}
}

func TestWantsEvent(t *testing.T) {
	ch := &models.NotificationChannel{Events: []string{EventAppDown, EventNodeOffline}, Projects: []string{"p1"}}
	cases := []struct {
		ev   Event
		want bool
	}{
		{Event{Type: EventAppDown, ProjectID: "p1"}, true},
		{Event{Type: EventAppDown, ProjectID: "p2"}, false},
		{Event{Type: EventNodeOffline}, true}, // node events aren't per project
		{Event{Type: EventDeployFailed, ProjectID: "p1"}, false},
		{Event{Type: EventTest}, true},
	}
	for _, c := range cases {
		if got := wantsEvent(ch, c.ev); got != c.want {
			t.Errorf("%s/%s: got %v", c.ev.Type, c.ev.ProjectID, got)
		}
	}
}

func TestEmit_DeliversOnceAndRecordsOutcome(t *testing.T) {
	srv, got := captureServer(t)
	db := testutil.NewDB(t, &models.NotificationChannel{})
	s := NewNotificationService(db, "https://hub.example.com")
	s.n.client = srv.Client()
	t.Cleanup(func() { hubNotifications = nil })
	db.Create(&models.NotificationChannel{ID: "c1", Name: "hook", Type: "webhook", Enabled: true,
		Config: models.SecretEnvVars{{Key: "url", Value: srv.URL}}, Events: []string{EventNodeOffline}})
	db.Create(&models.NotificationChannel{ID: "c2", Name: "off", Type: "webhook", Enabled: false,
		Config: models.SecretEnvVars{{Key: "url", Value: srv.URL}}, Events: []string{EventNodeOffline}})

	ev := Event{Type: EventNodeOffline, Level: LevelError, Title: "pc is offline", Node: "pc"}
	s.Emit(ev)
	s.Emit(ev) // a duplicate within a minute is dropped
	s.Emit(Event{Type: EventDeployFailed, Title: "not subscribed"})
	waitFor(t, func() bool { return len(got()) >= 1 })
	time.Sleep(100 * time.Millisecond)
	if n := len(got()); n != 1 {
		t.Fatalf("delivered %d times, want 1", n)
	}
	waitFor(t, func() bool {
		var ch models.NotificationChannel
		db.First(&ch, "id = ?", "c1")
		return ch.LastSentAt != nil
	})
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestHealth_AnnouncesDownThenBackUp(t *testing.T) {
	srv, got := captureServer(t)
	hs, _, db := newFailoverEnv(t)
	db.AutoMigrate(&models.NotificationChannel{})
	s := NewNotificationService(db, "")
	s.n.client = srv.Client()
	t.Cleanup(func() { hubNotifications = nil })
	db.Create(&models.NotificationChannel{ID: "c1", Name: "hook", Type: "webhook", Enabled: true,
		Config: models.SecretEnvVars{{Key: "url", Value: srv.URL}}, Events: []string{EventAppDown, EventAppRecovered}})

	p := loadProject(db)
	hs.handleUnhealthyProject(&p)
	hs.handleUnhealthyProject(&p) // still down: announced once
	waitFor(t, func() bool { return len(got()) == 1 })
	var ev Event
	json.Unmarshal(got()[0].body, &ev)
	if ev.Type != EventAppDown || ev.Project != "api" || !strings.Contains(ev.Message, "moved to good") {
		t.Fatalf("down event: %+v", ev)
	}

	// It answers again (on the node it moved to).
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer health.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(health.URL, "http://"))
	db.Model(&models.Node{}).Where("id = ?", "good").Update("vpn_ip", host)
	db.Model(&models.Project{}).Where("id = ?", "p1").Updates(map[string]interface{}{"node_id": "good", "status": "running"})
	db.Model(&models.Project{}).Where("id = ?", "p1").Select("ports").Updates(&models.Project{Ports: []string{port + ":8000"}})
	hs.checkAllProjects()
	waitFor(t, func() bool { return len(got()) == 2 })
	json.Unmarshal(got()[1].body, &ev)
	if ev.Type != EventAppRecovered || ev.Node != "good" {
		t.Fatalf("recovered event: %+v", ev)
	}
}

func TestLastLines_SkipsAgentSummary(t *testing.T) {
	logs := "pulling\nLogin Succeeded\n====================================\nCompleted at: 2026-09-27T10:31:27+01:00\nExit Code: 1\nDuration: 1167ms\nSTDOUT:\nLogin Succeeded\nSTDERR:\nError response from daemon: manifest unknown\n"
	if got := lastLines(logs, 2); got != "Login Succeeded\nError response from daemon: manifest unknown" {
		t.Errorf("got %q", got)
	}
}

func TestCustomNotificationPlugin(t *testing.T) {
	srv, got := captureServer(t)
	teams := NotificationType{
		ID: "teams", Name: "Microsoft Teams",
		Fields: []NotificationField{{Key: "url", Label: "Workflow URL", Secret: true, Required: true}, {Key: "token", Label: "Token"}},
		Request: &NotifyRequest{
			URL:     "{{.config.url}}/post",
			Headers: map[string]string{"X-Token": "{{.config.token}}"},
			Body:    `{"title": {{json .title}}, "text": {{json .text}}, "color": {{.color}}, "hex": {{json .hex}}}`,
		},
	}
	if err := validateCustomType(&teams); err != nil {
		t.Fatal(err)
	}
	bad := teams
	bad.Request = &NotifyRequest{URL: "{{.config.url"}
	if err := validateCustomType(&bad); err == nil {
		t.Error("broken template accepted")
	}

	db := testutil.NewDB(t, &models.Setting{}, &models.NotificationChannel{})
	if err := saveCustomNotificationTypes(db, []NotificationType{teams}); err != nil {
		t.Fatal(err)
	}
	s := NewNotificationService(db, "")
	s.n.client = srv.Client()
	t.Cleanup(func() { hubNotifications = nil })

	ch := models.NotificationChannel{ID: "c1", Type: "teams", Enabled: true}
	if err := s.apply(&ch, channelRequest{Config: []models.EnvVar{{Key: "url", Value: srv.URL}, {Key: "token", Value: "tk"}}, Events: []string{EventDeployFailed}}); err != nil {
		t.Fatal(err)
	}
	ev := testEvent
	ev.Title = `quote " and newline` + "\n"
	if err := s.deliver(&ch, ev); err != nil {
		t.Fatal(err)
	}
	r := got()[0]
	var body struct {
		Title string `json:"title"`
		Text  string `json:"text"`
		Color int    `json:"color"`
		Hex   string `json:"hex"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatalf("body isn't JSON: %v: %s", err, r.body)
	}
	if r.path != "/post" || r.header.Get("X-Token") != "tk" || r.header.Get("Content-Type") != "application/json" ||
		body.Title != ev.Title || body.Color != 0xef4444 || body.Hex != "#ef4444" || !strings.Contains(body.Text, "Node: pc") {
		t.Errorf("request %s %v %+v", r.path, r.header, body)
	}
	if v := viewChannel(ch, s.typeByID("teams")); configMap(v.Config)["url"] != models.MaskedValue {
		t.Error("secret field not masked")
	}
}
