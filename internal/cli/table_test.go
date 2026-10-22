package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"text/tabwriter"
)

func TestTableMatchesTabwriterWithoutColor(t *testing.T) {
	rows := "NODE\tSTATE\tAPPS\nasadullahs-laptop\tonline\t1\nabdullah-Precision-Tower-3430\tmaintenance\t22\nx\n\nSETTING\tVALUE\nkey\tv\n"

	var want bytes.Buffer
	tw := tabwriter.NewWriter(&want, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, rows)
	tw.Flush()

	var got bytes.Buffer
	tb := &table{out: &got}
	fmt.Fprint(tb, rows)
	tb.Flush()

	if got.String() != want.String() {
		t.Fatalf("table differs from tabwriter\n got: %q\nwant: %q", got.String(), want.String())
	}
}

func TestTableIgnoresColorCodes(t *testing.T) {
	e := &env{color: true}
	plain := &env{}
	render := func(e *env) string {
		var b bytes.Buffer
		tb := &table{out: &b}
		fmt.Fprintln(tb, "NODE\tSTATE\tAPPS")
		fmt.Fprintf(tb, "a\t%s\t1\n", e.paint(green, "online"))
		fmt.Fprintf(tb, "bbbbbb\t%s\t2\n", e.paint(red, "offline"))
		tb.Flush()
		return b.String()
	}
	if got, want := ansiCode.ReplaceAllString(render(e), ""), render(plain); got != want {
		t.Fatalf("color shifted the columns\n got: %q\nwant: %q", got, want)
	}
	if !strings.Contains(render(e), "\x1b[") {
		t.Fatal("expected colored output")
	}
}
