package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Notifications tell people what the Hub did or noticed (a deploy failed, a
// node went offline, an app moved) on Discord, Slack, Telegram, ntfy, email
// or any webhook. Channels are configured in the dashboard.

// Event types a channel can subscribe to.
const (
	EventDeploySucceeded = "deploy.succeeded"
	EventDeployFailed    = "deploy.failed"
	EventAppDown         = "app.down"
	EventAppRecovered    = "app.recovered"
	EventNodeOffline     = "node.offline"
	EventNodeOnline      = "node.online"
	EventNodeMaintenance = "node.maintenance"
	EventHubUpdate       = "hub.update"
	// EventTest is only sent by "Send test"; every channel accepts it.
	EventTest = "test"
)

type NotificationEventInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

var NotificationEvents = []NotificationEventInfo{
	{EventDeploySucceeded, "Deploys", "An app was deployed (a push, the dashboard or a move)."},
	{EventDeployFailed, "Failed deploys", "A deploy failed; the log's last lines are included."},
	{EventAppDown, "App down", "An app stopped answering its health checks and is being moved, or has nowhere to go."},
	{EventAppRecovered, "App back up", "An app that was down answers again."},
	{EventNodeOffline, "Node offline", "A node stopped sending heartbeats."},
	{EventNodeOnline, "Node back online", "An offline node is back."},
	{EventNodeMaintenance, "Maintenance", "A node was put into or taken out of maintenance mode."},
	{EventHubUpdate, "Hub updates", "A new ASDL Hub release is available."},
}

func knownEvent(id string) bool {
	for _, e := range NotificationEvents {
		if e.ID == id {
			return true
		}
	}
	return false
}

// Levels decide the colour and priority of a notification.
const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarning = "warning"
	LevelError   = "error"
)

type EventField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Event is one notification.
type Event struct {
	Type    string       `json:"event"`
	Level   string       `json:"level"`
	Title   string       `json:"title"`
	Message string       `json:"message"`
	Fields  []EventField `json:"fields,omitempty"`
	// Details is preformatted text (e.g. log lines), shown as code.
	Details   string    `json:"details,omitempty"`
	URL       string    `json:"url,omitempty"` // a dashboard link
	ProjectID string    `json:"project_id,omitempty"`
	Project   string    `json:"project,omitempty"`
	Node      string    `json:"node,omitempty"`
	Time      time.Time `json:"time"`
}

// NotificationField is a setting of a channel type.
type NotificationField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Default     string `json:"default,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type sendFunc func(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error

// NotificationType is a notification plugin: a kind of channel, like
// Discord. Built-in ones have code of their own; custom ones (added by an
// admin as JSON) describe the HTTP request to send in Request.
type NotificationType struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Fields      []NotificationField `json:"fields"`
	Request     *NotifyRequest      `json:"request,omitempty"`
	Builtin     bool                `json:"builtin,omitempty"`
	// check validates the settings beyond required fields.
	check func(cfg map[string]string) error
	send  sendFunc
}

var builtinNotificationTypes = []NotificationType{
	{
		ID:          "discord",
		Name:        "Discord",
		Description: "Posts to a Discord channel through a webhook (Channel settings → Integrations → Webhooks → New Webhook → Copy Webhook URL).",
		Fields: []NotificationField{
			{Key: "webhook_url", Label: "Webhook URL", Placeholder: "https://discord.com/api/webhooks/…", Secret: true, Required: true},
			{Key: "mention", Label: "Mention on errors (optional)", Placeholder: "<@&role id> or @here", Help: "Added to failed deploys and apps or nodes going down."},
		},
		check: func(cfg map[string]string) error {
			return checkURLPrefix(cfg["webhook_url"], "https://discord.com/api/webhooks/", "https://discordapp.com/api/webhooks/", "https://ptb.discord.com/api/webhooks/", "https://canary.discord.com/api/webhooks/")
		},
		send: sendDiscord,
	},
	{
		ID:          "slack",
		Name:        "Slack",
		Description: "Posts to a Slack channel through an incoming webhook (api.slack.com/apps → your app → Incoming Webhooks). Mattermost and Rocket.Chat webhooks work too.",
		Fields: []NotificationField{
			{Key: "webhook_url", Label: "Webhook URL", Placeholder: "https://hooks.slack.com/services/…", Secret: true, Required: true},
		},
		check: func(cfg map[string]string) error { return checkHTTPURL(cfg["webhook_url"]) },
		send:  sendSlack,
	},
	{
		ID:          "telegram",
		Name:        "Telegram",
		Description: "Messages a Telegram chat from your bot (create one with @BotFather, add it to the chat).",
		Fields: []NotificationField{
			{Key: "bot_token", Label: "Bot token", Placeholder: "123456:ABC…", Secret: true, Required: true},
			{Key: "chat_id", Label: "Chat ID", Placeholder: "-1001234567890 or @channelname", Required: true, Help: "Send the bot a message, then open api.telegram.org/bot<token>/getUpdates to find it."},
		},
		send: sendTelegram,
	},
	{
		ID:          "ntfy",
		Name:        "ntfy",
		Description: "Push notifications to your phone or desktop with the ntfy app (ntfy.sh or your own server), no account needed.",
		Fields: []NotificationField{
			{Key: "topic", Label: "Topic", Placeholder: "asdl-7f3k2q", Required: true, Help: "Anyone who knows the topic can read it on ntfy.sh, so pick something hard to guess."},
			{Key: "server", Label: "Server", Default: "https://ntfy.sh"},
			{Key: "token", Label: "Access token (optional)", Secret: true},
		},
		check: func(cfg map[string]string) error {
			if strings.ContainsAny(cfg["topic"], "/?# ") {
				return fmt.Errorf("the topic can't contain '/', '?', '#' or spaces")
			}
			return checkHTTPURL(cfg["server"])
		},
		send: sendNtfy,
	},
	{
		ID:          "email",
		Name:        "Email",
		Description: "Sends an email through your SMTP server (Gmail, Fastmail, Resend, your own…).",
		Fields: []NotificationField{
			{Key: "to", Label: "To", Placeholder: "you@example.com, team@example.com", Required: true},
			{Key: "from", Label: "From", Placeholder: "ASDL Hub <hub@example.com>", Required: true},
			{Key: "host", Label: "SMTP server", Placeholder: "smtp.gmail.com", Required: true},
			{Key: "port", Label: "Port", Default: "587", Help: "587 (STARTTLS) or 465 (TLS)."},
			{Key: "username", Label: "Username"},
			{Key: "password", Label: "Password", Secret: true, Help: "For Gmail, an app password."},
		},
		check: func(cfg map[string]string) error {
			if p, err := strconv.Atoi(cfg["port"]); err != nil || p < 1 || p > 65535 {
				return fmt.Errorf("port must be a number")
			}
			if _, err := parseAddressList(cfg["to"]); err != nil {
				return err
			}
			_, err := parseAddressList(cfg["from"])
			return err
		},
		send: sendEmail,
	},
	{
		ID:          "webhook",
		Name:        "Webhook",
		Description: "POSTs the event as JSON to any URL, for your own automations (n8n, Zapier, a script…).",
		Fields: []NotificationField{
			{Key: "url", Label: "URL", Placeholder: "https://example.com/hooks/asdl", Secret: true, Required: true},
			{Key: "secret", Label: "Signing secret (optional)", Secret: true, Help: "If set, X-ASDL-Signature: sha256=<HMAC-SHA256 of the body> is sent."},
		},
		check: func(cfg map[string]string) error { return checkHTTPURL(cfg["url"]) },
		send:  sendWebhook,
	},
}

func init() {
	for i := range builtinNotificationTypes {
		builtinNotificationTypes[i].Builtin = true
	}
}

func checkHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}
	return nil
}

func checkURLPrefix(raw string, prefixes ...string) error {
	for _, p := range prefixes {
		if strings.HasPrefix(raw, p) {
			return nil
		}
	}
	return fmt.Errorf("that is not a Discord webhook URL (it starts with %s)", prefixes[0])
}

// notifier holds what senders share.
type notifier struct {
	client *http.Client
	// hubURL is the dashboard's public address, for links and the avatar.
	hubURL string
}

func (n *notifier) postJSON(ctx context.Context, target string, body interface{}, header http.Header) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ASDL-Hub")
	for k, v := range header {
		req.Header[k] = v
	}
	return n.do(req)
}

func (n *notifier) do(req *http.Request) error {
	resp, err := n.client.Do(req)
	if err != nil {
		// The error text contains the URL, which is a secret for webhooks.
		if ue, ok := err.(*url.Error); ok {
			return fmt.Errorf("could not reach %s: %v", req.URL.Host, ue.Err)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

var levelColors = map[string]int{
	LevelInfo:    0xf29a00, // ASDL orange
	LevelSuccess: 0x22c55e,
	LevelWarning: 0xf59e0b,
	LevelError:   0xef4444,
}

var levelEmoji = map[string]string{
	LevelInfo:    "ℹ️",
	LevelSuccess: "✅",
	LevelWarning: "⚠️",
	LevelError:   "🔴",
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func sendDiscord(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	embed := map[string]interface{}{
		"title":       truncate(ev.Title, 256),
		"description": truncate(ev.Message, 3500),
		"color":       levelColors[ev.Level],
		"timestamp":   ev.Time.UTC().Format(time.RFC3339),
		"footer":      map[string]string{"text": "ASDL Hub"},
	}
	if ev.Details != "" {
		embed["description"] = truncate(ev.Message, 2000) + "\n```\n" + truncate(strings.ReplaceAll(ev.Details, "```", "'''"), 1400) + "\n```"
	}
	if ev.URL != "" {
		embed["url"] = ev.URL
	}
	fields := []map[string]interface{}{}
	for _, f := range ev.Fields {
		fields = append(fields, map[string]interface{}{"name": truncate(f.Name, 256), "value": truncate(f.Value, 1024), "inline": true})
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	body := map[string]interface{}{
		"username":         "ASDL Hub",
		"embeds":           []interface{}{embed},
		"allowed_mentions": map[string]interface{}{"parse": []string{"roles", "users", "everyone"}},
	}
	if n.hubURL != "" {
		body["avatar_url"] = n.hubURL + "/apple-icon.png"
	}
	if m := strings.TrimSpace(cfg["mention"]); m != "" && (ev.Level == LevelError || ev.Type == EventTest) {
		body["content"] = m
	}
	return n.postJSON(ctx, cfg["webhook_url"], body, nil)
}

func sendSlack(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	text := ev.Message
	if ev.Details != "" {
		text += "\n```" + truncate(ev.Details, 2500) + "```"
	}
	att := map[string]interface{}{
		"color":     fmt.Sprintf("#%06x", levelColors[ev.Level]),
		"title":     ev.Title,
		"text":      text,
		"footer":    "ASDL Hub",
		"ts":        ev.Time.Unix(),
		"mrkdwn_in": []string{"text"},
	}
	if ev.URL != "" {
		att["title_link"] = ev.URL
	}
	var fields []map[string]interface{}
	for _, f := range ev.Fields {
		fields = append(fields, map[string]interface{}{"title": f.Name, "value": f.Value, "short": true})
	}
	if len(fields) > 0 {
		att["fields"] = fields
	}
	body := map[string]interface{}{
		// Shown in phone and desktop notifications.
		"text":        levelEmoji[ev.Level] + " " + ev.Title,
		"attachments": []interface{}{att},
	}
	return n.postJSON(ctx, cfg["webhook_url"], body, nil)
}

// telegramAPI is Telegram's Bot API; tests point it elsewhere.
var telegramAPI = "https://api.telegram.org"

func sendTelegram(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b>\n%s", levelEmoji[ev.Level], html.EscapeString(ev.Title), html.EscapeString(ev.Message))
	for _, f := range ev.Fields {
		fmt.Fprintf(&b, "\n<b>%s:</b> %s", html.EscapeString(f.Name), html.EscapeString(f.Value))
	}
	if ev.Details != "" {
		fmt.Fprintf(&b, "\n<pre>%s</pre>", html.EscapeString(truncate(ev.Details, 2500)))
	}
	if ev.URL != "" {
		fmt.Fprintf(&b, "\n<a href=\"%s\">Open in ASDL Hub</a>", html.EscapeString(ev.URL))
	}
	body := map[string]interface{}{
		"chat_id":                  cfg["chat_id"],
		"text":                     truncate(b.String(), 4000),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	}
	err := n.postJSON(ctx, telegramAPI+"/bot"+cfg["bot_token"]+"/sendMessage", body, nil)
	if err != nil {
		// Never show the token (it's in the URL).
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), cfg["bot_token"], "<token>"))
	}
	return nil
}

var ntfyPriority = map[string]string{LevelInfo: "3", LevelSuccess: "3", LevelWarning: "4", LevelError: "5"}
var ntfyTags = map[string]string{LevelInfo: "information_source", LevelSuccess: "white_check_mark", LevelWarning: "warning", LevelError: "rotating_light"}

func sendNtfy(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	server := strings.TrimRight(cfg["server"], "/")
	if server == "" {
		server = "https://ntfy.sh"
	}
	msg := ev.Message
	for _, f := range ev.Fields {
		msg += fmt.Sprintf("\n%s: %s", f.Name, f.Value)
	}
	if ev.Details != "" {
		msg += "\n\n" + truncate(ev.Details, 1500)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/"+url.PathEscape(cfg["topic"]), strings.NewReader(msg))
	if err != nil {
		return err
	}
	// Header values must be ASCII-safe; ntfy decodes RFC 2047 for the title.
	req.Header.Set("Title", mimeHeader(ev.Title))
	req.Header.Set("Priority", ntfyPriority[ev.Level])
	req.Header.Set("Tags", ntfyTags[ev.Level])
	if ev.URL != "" {
		req.Header.Set("Click", ev.URL)
	}
	if n.hubURL != "" {
		req.Header.Set("Icon", n.hubURL+"/apple-icon.png")
	}
	if t := cfg["token"]; t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	return n.do(req)
}

func sendWebhook(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg["url"], bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ASDL-Hub")
	req.Header.Set("X-ASDL-Event", ev.Type)
	if s := cfg["secret"]; s != "" {
		req.Header.Set("X-ASDL-Signature", "sha256="+signBody(s, b))
	}
	return n.do(req)
}

func signBody(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
