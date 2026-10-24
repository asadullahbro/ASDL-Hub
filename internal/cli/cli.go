package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

const usage = `ASDL Hub %s

Usage: asdl-hub <command> [arguments]

Overview
  status                      Hub version, nodes, apps and today's jobs
  doctor                      Check everything and say what to fix
  nodes                       Nodes with their state, agent and resources
  apps                        Apps (projects): where they run and their health
  app <app>                   One app in detail, with its plugins

Apps
  deploy <app>                Redeploy an app with its current image
  app set <app> key=value     Change domain, path, image, ports, node or description
  env <app>                   An app's environment variable names
  env set <app> [KEY]         Add or change variables; values are asked for
                              at a hidden prompt (the app redeploys)
  env unset <app> [KEY]       Remove variables
  move <app> [node]           Move an app to another node (without one, the
                              best node is suggested and you are asked)
  rollback <app>              Put an app's previous version back
  restart <app>               Restart an app's container
  logs <app> [-n lines] [-f]  An app's recent logs; -f keeps following (up to 10 min)

Nodes
  outages <node>              A node's outages, their likely cause, and when they happen
  maintenance <node> on|off   Move apps off a node before working on it, or end that
  node remove <node>          Forget a node for good (move its apps off first)

Jobs and alerts
  jobs [-n count]             Recent jobs
  job <id>                    A job's status and output
  notify                      Notification channels
  notify test <channel>       Send a test notification

Hub server (run these on it, with sudo)
  config                      The Hub's settings (/opt/asdl-hub/.env)
  config set [KEY]            Change settings (asked for; secrets hidden),
                              then restart the Hub
  config unset [KEY]          Remove a setting
  server status|restart|logs  The Hub's service; logs -f follows it
  nginx reload                Rewrite app routes and reload nginx
  nginx test|routes           Check nginx's config, or show the app routes

Hub
  update                      Is a new release out?
  update install              Install the latest release
  login <hub url>             Sign in to a Hub (saved in ~/.config/asdl-hub)
  logout                      Forget that login
  whoami                      Which Hub and user commands go to
  version                     This program's version
  serve                       Run the Hub server (what the service runs)

Apps and nodes are named by name or ID. Add --json to print the API's answer.
On the Hub's server, run commands with sudo; elsewhere, log in first.
Docs: https://docs.asdl.website/hub/cli/
`

type env struct {
	out     io.Writer
	version string
	json    bool
	color   bool
}

// IsCommand reports whether args ask for a CLI command rather than the server.
func IsCommand(args []string) bool {
	if len(args) == 0 {
		// Services start the Hub without arguments; people get help.
		// Services start with no terminal on stdin. (INVOCATION_ID can't
		// tell them apart: desktops start terminals under systemd too.)
		return term.IsTerminal(int(os.Stdin.Fd()))
	}
	return args[0] != "serve"
}

// Run runs one command and returns the exit code.
func Run(args []string, version string) int {
	e := &env{out: os.Stdout, version: version, color: term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == ""}
	var rest []string
	for _, a := range args {
		if a == "--json" {
			e.json = true
			continue
		}
		rest = append(rest, a)
	}
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" || rest[0] == "--help" {
		fmt.Fprintf(e.out, usage, version)
		return 0
	}
	cmd, cmdArgs := rest[0], rest[1:]
	if err := e.run(cmd, cmdArgs); err != nil {
		if err == errSilent {
			return 1
		}
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "asdl-hub %s: %s\nRun `asdl-hub help` for the commands.\n", cmd, ue.msg)
			return 2
		}
		fmt.Fprintf(os.Stderr, "asdl-hub %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func needArgs(args []string, n int, what string) error {
	if len(args) < n {
		return usageError{"expected " + what}
	}
	return nil
}

func (e *env) run(cmd string, args []string) error {
	switch cmd {
	case "version", "--version", "-v":
		fmt.Fprintln(e.out, "asdl-hub", e.version)
		return nil
	case "login":
		return e.login(args)
	case "logout":
		return e.logout()
	case "doctor":
		return e.doctor()
	case "config":
		return e.config(args)
	case "server", "service":
		return e.server(args)
	case "nginx":
		return e.nginx(args)
	}

	c, err := newClient()
	if err != nil {
		return err
	}
	switch cmd {
	case "status":
		return e.status(c)
	case "nodes":
		return e.nodes(c)
	case "apps", "projects":
		return e.apps(c)
	case "app", "project":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		if args[0] == "set" {
			if err := needArgs(args, 2, "asdl-hub app set <app> key=value ..."); err != nil {
				return err
			}
			return e.appSet(c, args[1], args[2:])
		}
		return e.app(c, args[0])
	case "env":
		return e.envCmd(c, args)
	case "deploy", "redeploy":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		return e.deploy(c, args[0])
	case "move":
		if err := needArgs(args, 1, "an app, and optionally a node: asdl-hub move <app> [node]"); err != nil {
			return err
		}
		to := ""
		if len(args) > 1 {
			to = args[1]
		}
		return e.move(c, args[0], to)
	case "rollback":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		return e.rollback(c, args[0])
	case "restart":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		return e.restart(c, args[0])
	case "logs":
		fs := flag.NewFlagSet("logs", flag.ContinueOnError)
		n := fs.Int("n", 100, "lines")
		f := fs.Bool("f", false, "follow")
		app, err := parseWithPositional(fs, args, 1, "an app name")
		if err != nil {
			return err
		}
		return e.logs(c, app[0], *n, *f)
	case "node":
		if len(args) < 2 || args[0] != "remove" {
			return usageError{"expected asdl-hub node remove <node> [--yes]"}
		}
		yes := false
		var name string
		for _, a := range args[1:] {
			if a == "--yes" || a == "-y" {
				yes = true
			} else {
				name = a
			}
		}
		return e.removeNode(c, name, yes)
	case "outages":
		if len(args) < 1 {
			return usageError{"expected asdl-hub outages <node>"}
		}
		return e.outages(c, args[0])
	case "maintenance":
		if len(args) < 2 || (args[1] != "on" && args[1] != "off") {
			return usageError{"expected asdl-hub maintenance <node> on|off"}
		}
		return e.maintenance(c, args[0], args[1] == "on")
	case "jobs":
		fs := flag.NewFlagSet("jobs", flag.ContinueOnError)
		n := fs.Int("n", 15, "count")
		if _, err := parseWithPositional(fs, args, 0, ""); err != nil {
			return err
		}
		return e.jobs(c, *n)
	case "job":
		if err := needArgs(args, 1, "a job ID"); err != nil {
			return err
		}
		return e.job(c, args[0])
	case "notify", "notifications":
		if len(args) >= 2 && args[0] == "test" {
			return e.notifyTest(c, strings.Join(args[1:], " "))
		}
		return e.notifyList(c)
	case "update":
		if len(args) > 0 && args[0] == "install" {
			return e.updateInstall(c)
		}
		return e.updateStatus(c)
	case "whoami":
		return e.whoami(c)
	}
	return usageError{fmt.Sprintf("unknown command %q", cmd)}
}

// parseWithPositional parses flags that may come before or after the
// positional arguments (asdl-hub logs api -n 50 or logs -n 50 api).
func parseWithPositional(fs *flag.FlagSet, args []string, want int, what string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, usageError{err.Error()}
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) < want {
		return nil, usageError{"expected " + what}
	}
	return pos, nil
}

func (e *env) table() *table {
	return &table{out: e.out}
}

var ansiCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// table aligns tab-separated columns like text/tabwriter with two spaces of padding, but measures
// a cell without its color codes, which tabwriter would count as visible width.
type table struct {
	out io.Writer
	buf bytes.Buffer
}

func (t *table) Write(p []byte) (int, error) { return t.buf.Write(p) }

func (t *table) Flush() error {
	text := t.buf.String()
	t.buf.Reset()
	if text == "" {
		return nil
	}
	endsLine := strings.HasSuffix(text, "\n")
	rows := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	cells := make([][]string, len(rows))
	maxCols := 0
	for i, row := range rows {
		cells[i] = strings.Split(row, "\t")
		maxCols = max(maxCols, len(cells[i])-1)
	}

	// A column's width is shared by the consecutive rows that end a cell in it with a tab.
	widths := make([][]int, len(rows))
	for i := range widths {
		widths[i] = make([]int, maxCols)
	}
	for col := 0; col < maxCols; col++ {
		for start := 0; start < len(rows); {
			if len(cells[start])-1 <= col {
				start++
				continue
			}
			end, width := start, 0
			for ; end < len(rows) && len(cells[end])-1 > col; end++ {
				width = max(width, visibleWidth(cells[end][col])+2)
			}
			for i := start; i < end; i++ {
				widths[i][col] = width
			}
			start = end
		}
	}

	var out strings.Builder
	for i, row := range cells {
		for col, cell := range row {
			out.WriteString(cell)
			if col < len(row)-1 {
				out.WriteString(strings.Repeat(" ", widths[i][col]-visibleWidth(cell)))
			}
		}
		if i < len(cells)-1 || endsLine {
			out.WriteByte('\n')
		}
	}
	_, err := io.WriteString(t.out, out.String())
	return err
}

func visibleWidth(s string) int {
	return utf8.RuneCountInString(ansiCode.ReplaceAllString(s, ""))
}

const (
	green  = "32"
	red    = "31"
	yellow = "33"
	dim    = "90"
)

func (e *env) paint(code, s string) string {
	if !e.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
