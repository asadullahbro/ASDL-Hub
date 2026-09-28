package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

const usage = `ASDL Hub %s

Usage: asdl-hub <command> [arguments]

Overview
  status                      Hub version, nodes, apps and today's jobs
  nodes                       Nodes with their state, agent and resources
  apps                        Apps (projects): where they run and their health
  app <app>                   One app in detail, with its plugins

Apps
  deploy <app>                Redeploy an app with its current image
  move <app> <node>           Move an app to another node
  restart <app>               Restart an app's container
  logs <app> [-n lines]       An app's recent logs

Nodes
  maintenance <node> on|off   Move apps off a node before working on it, or end that

Jobs and alerts
  jobs [-n count]             Recent jobs
  job <id>                    A job's status and output
  notify                      Notification channels
  notify test <channel>       Send a test notification

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
		return term.IsTerminal(int(os.Stdin.Fd())) && os.Getenv("INVOCATION_ID") == ""
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
		return e.app(c, args[0])
	case "deploy", "redeploy":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		return e.deploy(c, args[0])
	case "move":
		if err := needArgs(args, 2, "an app and a node: asdl-hub move <app> <node>"); err != nil {
			return err
		}
		return e.move(c, args[0], args[1])
	case "restart":
		if err := needArgs(args, 1, "an app name"); err != nil {
			return err
		}
		return e.restart(c, args[0])
	case "logs":
		fs := flag.NewFlagSet("logs", flag.ContinueOnError)
		n := fs.Int("n", 100, "lines")
		app, err := parseWithPositional(fs, args, 1, "an app name")
		if err != nil {
			return err
		}
		return e.logs(c, app[0], *n)
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

func (e *env) table() *tabwriter.Writer {
	return tabwriter.NewWriter(e.out, 0, 0, 2, ' ', 0)
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
