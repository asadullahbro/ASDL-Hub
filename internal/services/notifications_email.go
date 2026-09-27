package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

func parseAddressList(s string) ([]*mail.Address, error) {
	list, err := mail.ParseAddressList(s)
	if err != nil {
		return nil, fmt.Errorf("%q is not a list of email addresses", s)
	}
	return list, nil
}

// mimeHeader encodes s for a mail or HTTP header if it isn't plain ASCII.
func mimeHeader(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

func sendEmail(ctx context.Context, n *notifier, cfg map[string]string, ev Event) error {
	from, err := parseAddressList(cfg["from"])
	if err != nil {
		return err
	}
	to, err := parseAddressList(cfg["to"])
	if err != nil {
		return err
	}
	msg := buildEmail(from[0], to, ev)

	host, port := cfg["host"], cfg["port"]
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(host, port)
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if port == "465" {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: host})
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("could not reach %s: %v", addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if port != "465" {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}
	if cfg["username"] != "" {
		// PlainAuth refuses to send the password without TLS (except to
		// localhost), which is what we want.
		if err := c.Auth(smtp.PlainAuth("", cfg["username"], cfg["password"], host)); err != nil {
			return fmt.Errorf("SMTP login failed: %v", err)
		}
	}
	if err := c.Mail(from[0].Address); err != nil {
		return err
	}
	for _, a := range to {
		if err := c.Rcpt(a.Address); err != nil {
			return fmt.Errorf("%s: %v", a.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildEmail(from *mail.Address, to []*mail.Address, ev Event) []byte {
	var text, body strings.Builder
	text.WriteString(ev.Message + "\n")
	fmt.Fprintf(&body, `<div style="font-family:system-ui,sans-serif;max-width:600px">`+
		`<div style="border-left:4px solid #%06x;padding:4px 14px">`+
		`<h2 style="margin:0 0 8px;font-size:18px">%s</h2><p style="margin:0 0 12px">%s</p>`,
		levelColors[ev.Level], html.EscapeString(ev.Title), html.EscapeString(ev.Message))
	if len(ev.Fields) > 0 {
		body.WriteString(`<table style="border-collapse:collapse;font-size:14px">`)
		for _, f := range ev.Fields {
			fmt.Fprintf(&text, "%s: %s\n", f.Name, f.Value)
			fmt.Fprintf(&body, `<tr><td style="color:#666;padding:2px 12px 2px 0">%s</td><td>%s</td></tr>`,
				html.EscapeString(f.Name), html.EscapeString(f.Value))
		}
		body.WriteString(`</table>`)
	}
	if ev.Details != "" {
		fmt.Fprintf(&text, "\n%s\n", ev.Details)
		fmt.Fprintf(&body, `<pre style="background:#f4f4f5;padding:10px;font-size:12px;white-space:pre-wrap">%s</pre>`, html.EscapeString(ev.Details))
	}
	if ev.URL != "" {
		fmt.Fprintf(&text, "\n%s\n", ev.URL)
		fmt.Fprintf(&body, `<p><a href="%s">Open in ASDL Hub</a></p>`, html.EscapeString(ev.URL))
	}
	body.WriteString(`</div><p style="color:#999;font-size:12px">Sent by ASDL Hub</p></div>`)

	addrs := make([]string, len(to))
	for i, a := range to {
		addrs[i] = a.String()
	}
	boundary := fmt.Sprintf("asdl-%d", ev.Time.UnixNano())
	var m strings.Builder
	fmt.Fprintf(&m, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		from.String(), strings.Join(addrs, ", "), mimeHeader("[ASDL Hub] "+ev.Title), ev.Time.Format(time.RFC1123Z))
	fmt.Fprintf(&m, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", boundary)
	for _, part := range []struct{ typ, s string }{{"text/plain", text.String()}, {"text/html", body.String()}} {
		fmt.Fprintf(&m, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n", boundary, part.typ,
			strings.ReplaceAll(strings.ReplaceAll(part.s, "\r\n", "\n"), "\n", "\r\n"))
	}
	fmt.Fprintf(&m, "--%s--\r\n", boundary)
	return []byte(m.String())
}
