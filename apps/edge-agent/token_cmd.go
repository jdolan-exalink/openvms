package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jdolan-exalink/openvms/internal/agent"
	"github.com/jdolan-exalink/openvms/internal/agent/tokens"
)

// tokensFile is where the agent's console tokens live: OPENVMS_AGENT_TOKENS_FILE, or next to the
// legacy token file.
func tokensFile() string {
	if v := env("OPENVMS_AGENT_TOKENS_FILE", ""); v != "" {
		return v
	}
	return filepath.Join(filepath.Dir(env("OPENVMS_AGENT_TOKEN_FILE", "/etc/openvms/agent.token")), "agent.tokens.json")
}

// runConsoleCommand runs `openvms-agent token ...` and `openvms-agent connection`. Anything meant to
// be piped (a new token) goes to stdout alone; explanations go to stderr. It returns the exit code.
func runConsoleCommand(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, consoleUsage)
		return 2
	}
	switch args[0] {
	case "token":
		return runTokenCommand(args[1:], stdout, stderr, now)
	case "connection":
		return runConnectionCommand(args[1:], stdout, stderr, now)
	}
	fmt.Fprintln(stderr, consoleUsage)
	return 2
}

const consoleUsage = `usage:
  openvms-agent token create --name NAME --scopes read,events,control,update|all [--ttl 720h]
  openvms-agent token list
  openvms-agent token revoke ID|NAME
  openvms-agent token rotate ID|NAME
  openvms-agent connection [--port 7419]      create the "openvms" token and print what to enter in OpenVMS
every command takes --file PATH (default: next to the agent token, ` + "agent.tokens.json" + `)`

func newConsoleFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "token file (default: next to the agent token)")
	return fs, file
}

// parseInterspersed parses flags that may come before or after the positional arguments, so both
// `token rotate central --file F` and `token rotate --file F central` work.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func openTokens(file string) *tokens.Store {
	if file == "" {
		file = tokensFile()
	}
	return tokens.Open(file)
}

func runTokenCommand(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, consoleUsage)
		return 2
	}
	sub, rest := args[0], args[1:]
	fs, file := newConsoleFlags("token "+sub, stderr)
	switch sub {
	case "create":
		name := fs.String("name", "", "token name (lowercase letters, digits, . _ -)")
		scopes := fs.String("scopes", "", "read,events,control,update or all")
		ttl := fs.String("ttl", "", "lifetime such as 720h; empty means it does not expire")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		list, err := tokens.ParseScopes(*scopes)
		if err != nil {
			fmt.Fprintln(stderr, "token create:", err)
			return 1
		}
		var d time.Duration
		if *ttl != "" {
			if d, err = time.ParseDuration(*ttl); err != nil {
				fmt.Fprintln(stderr, "token create: --ttl must look like 720h:", err)
				return 1
			}
		}
		secret, tok, err := openTokens(*file).Create(*name, list, d, now())
		if err != nil {
			fmt.Fprintln(stderr, "token create:", err)
			return 1
		}
		fmt.Fprintf(stderr, "Created token %q (%s). Copy it now; it will not be shown again.\n", tok.Name, tok.ID)
		fmt.Fprintln(stdout, secret)
		return 0
	case "list":
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		list, err := openTokens(*file).List()
		if err != nil {
			fmt.Fprintln(stderr, "token list:", err)
			return 1
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "No tokens. The token in the agent token file still works with every scope.")
			return 0
		}
		w := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME\tSCOPES\tCREATED\tEXPIRES")
		for _, t := range list {
			scopes := make([]string, len(t.Scopes))
			for i, s := range t.Scopes {
				scopes[i] = string(s)
			}
			expires := "never"
			if t.ExpiresAt != nil {
				expires = t.ExpiresAt.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Name, strings.Join(scopes, ","), t.CreatedAt.Format(time.RFC3339), expires)
		}
		_ = w.Flush()
		return 0
	case "revoke", "rotate":
		positional, err := parseInterspersed(fs, rest)
		if err != nil {
			return 2
		}
		if len(positional) != 1 {
			fmt.Fprintf(stderr, "token %s: give the token ID or name\n", sub)
			return 2
		}
		store := openTokens(*file)
		if sub == "revoke" {
			if err := store.Revoke(positional[0]); err != nil {
				fmt.Fprintln(stderr, "token revoke:", err)
				return 1
			}
			fmt.Fprintf(stderr, "Revoked %q.\n", positional[0])
			return 0
		}
		secret, tok, err := store.Rotate(positional[0], now())
		if err != nil {
			fmt.Fprintln(stderr, "token rotate:", err)
			return 1
		}
		fmt.Fprintf(stderr, "Rotated token %q (%s). The old secret stopped working; copy the new one now.\n", tok.Name, tok.ID)
		fmt.Fprintln(stdout, secret)
		return 0
	}
	fmt.Fprintln(stderr, consoleUsage)
	return 2
}

// runConnectionCommand creates the token OpenVMS uses to reach this agent and prints what to enter
// in OpenVMS. It never replaces an existing one: a second run points at `token rotate`.
func runConnectionCommand(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs, file := newConsoleFlags("connection", stderr)
	port := fs.Int("port", 7419, "port the agent listens on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	secret, _, err := openTokens(*file).Create("openvms", tokens.AllScopes, 0, now())
	if err != nil {
		fmt.Fprintln(stderr, "connection:", err)
		fmt.Fprintln(stderr, `If the "openvms" token exists, replace its secret with: openvms-agent token rotate openvms`)
		return 1
	}
	host := firstLANAddress()
	fmt.Fprintf(stdout, "OpenVMS agent connection\n")
	fmt.Fprintf(stdout, "  Agent version  %s\n", agent.Version)
	fmt.Fprintf(stdout, "  Address        http://%s\n", net.JoinHostPort(host, fmt.Sprint(*port)))
	fmt.Fprintf(stdout, "                 (use the address OpenVMS reaches: a VPN address, or https://name if you publish it through a tunnel)\n")
	fmt.Fprintf(stdout, "  Token          %s\n", secret)
	fmt.Fprintf(stdout, "Copy the token now; it will not be shown again.\n")
	return 0
}

// firstLANAddress returns this host's first non-loopback IPv4 address, or a placeholder.
func firstLANAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "<this-host>"
}
