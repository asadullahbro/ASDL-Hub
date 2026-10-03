package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
)

var stdin = bufio.NewReader(os.Stdin)

func interactive() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// ask reads one line from the person at the terminal.
func (e *env) ask(question string) (string, error) {
	fmt.Fprint(e.out, question)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("cancelled")
	}
	return strings.TrimSpace(line), nil
}

// askValue asks for a value; secret ones aren't shown while typed and so
// never reach the screen or the shell's history.
func (e *env) askValue(key string, secret bool) (string, error) {
	if !interactive() {
		// Piped in: printf '%s' "$TOKEN" | asdl-hub env set app TOKEN
		line, err := stdin.ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("no value for %s on standard input", key)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if !secret {
		return e.ask(fmt.Sprintf("Value for %s: ", key))
	}
	fmt.Fprintf(e.out, "Value for %s (hidden): ", key)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(e.out)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", fmt.Errorf("no value typed for %s", key)
	}
	fmt.Fprintf(e.out, "  %s %d characters\n", e.paint(green, "✓"), len([]rune(string(b))))
	return string(b), nil
}

func (e *env) confirm(question string) bool {
	if !interactive() {
		return true
	}
	a, err := e.ask(question + " [Y/n] ")
	if err != nil {
		return false
	}
	a = strings.ToLower(a)
	return a == "" || a == "y" || a == "yes"
}

// askChanges turns arguments into changes, asking for whatever is missing:
//
//	KEY=VALUE  used as given (in a terminal, a reminder that it is now in
//	           the shell's history)
//	KEY        the value is asked for at a prompt (hidden for secrets)
//	nothing    a short session: which names to change, then their values
//
// For unset, a nil value means remove.
func (e *env) askChanges(sub string, args []string, existing []string, secret func(string) bool, what string, valid func(string) error) (map[string]*string, error) {
	changes := map[string]*string{}
	add := func(k string, v *string) error {
		if err := valid(k); err != nil {
			return err
		}
		changes[k] = v
		return nil
	}
	if len(args) == 0 {
		if !interactive() {
			return nil, usageError{"name the " + what + " to " + sub}
		}
		if len(existing) > 0 {
			names := append([]string(nil), existing...)
			sort.Strings(names)
			fmt.Fprintf(e.out, "Current %ss: %s\n", what, strings.Join(names, ", "))
		}
		q := fmt.Sprintf("Which %s do you want to add or change? (Enter when done) ", what)
		if sub == "unset" {
			q = fmt.Sprintf("Which %s do you want to remove? (Enter when done) ", what)
		}
		for {
			k, err := e.ask(q)
			if err != nil {
				return nil, err
			}
			if k == "" {
				break
			}
			if sub == "unset" {
				if err := add(k, nil); err != nil {
					fmt.Fprintln(e.out, "  "+err.Error())
				}
				continue
			}
			if err := valid(k); err != nil {
				fmt.Fprintln(e.out, "  "+err.Error())
				continue
			}
			v, err := e.askValue(k, secret(k))
			if err != nil {
				fmt.Fprintln(e.out, "  "+err.Error())
				continue
			}
			changes[k] = &v
		}
		return changes, nil
	}
	warned := false
	for _, a := range args {
		k, v, hasValue := strings.Cut(a, "=")
		if sub == "unset" {
			if err := add(a, nil); err != nil {
				return nil, usageError{err.Error()}
			}
			continue
		}
		if !hasValue {
			if err := valid(k); err != nil {
				return nil, usageError{err.Error()}
			}
			val, err := e.askValue(k, secret(k))
			if err != nil {
				return nil, err
			}
			v = val
		} else if secret(k) && interactive() && !warned {
			fmt.Fprintf(e.out, "%s values typed in the command are kept in your shell's history. To type one at a hidden prompt, give only its name: %s\n",
				e.paint(yellow, "note:"), k)
			warned = true
		}
		value := v
		if err := add(k, &value); err != nil {
			return nil, usageError{err.Error()}
		}
	}
	return changes, nil
}

func changeNames(changes map[string]*string) string {
	var set, removed []string
	for k, v := range changes {
		if v == nil {
			removed = append(removed, k)
		} else {
			set = append(set, k)
		}
	}
	sort.Strings(set)
	sort.Strings(removed)
	var parts []string
	if len(set) > 0 {
		parts = append(parts, "set "+strings.Join(set, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "remove "+strings.Join(removed, ", "))
	}
	return strings.Join(parts, "; ")
}
