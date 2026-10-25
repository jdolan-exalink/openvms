package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
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
		if tok.Name == openvmsTokenName {
			refreshKeyFile(*file, secret, stderr)
		}
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
			revokesOpenVMS := isOpenVMSToken(store, positional[0])
			if err := store.Revoke(positional[0]); err != nil {
				fmt.Fprintln(stderr, "token revoke:", err)
				return 1
			}
			fmt.Fprintf(stderr, "Revoked %q.\n", positional[0])
			if revokesOpenVMS {
				// The saved key must never keep a secret that no longer works.
				path := openvmsKeyFile(*file)
				if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(stderr, "could not remove the old key %s: %v\n", path, err)
				}
			}
			return 0
		}
		secret, tok, err := store.Rotate(positional[0], now())
		if err != nil {
			fmt.Fprintln(stderr, "token rotate:", err)
			return 1
		}
		fmt.Fprintf(stderr, "Rotated token %q (%s). The old secret stopped working; copy the new one now.\n", tok.Name, tok.ID)
		if tok.Name == openvmsTokenName {
			refreshKeyFile(*file, secret, stderr)
		}
		fmt.Fprintln(stdout, secret)
		return 0
	}
	fmt.Fprintln(stderr, consoleUsage)
	return 2
}

// openvmsTokenName is the token OpenVMS uses to reach this agent.
const openvmsTokenName = "openvms"

// isOpenVMSToken reports whether ref (an ID or a name) points at the OpenVMS token.
func isOpenVMSToken(store *tokens.Store, ref string) bool {
	list, err := store.List()
	if err != nil {
		return false
	}
	for _, t := range list {
		if t.ID == ref || t.Name == ref {
			return t.Name == openvmsTokenName
		}
	}
	return false
}

// refreshKeyFile keeps the saved key equal to the secret that works: the file must never hold a
// secret that no longer works.
func refreshKeyFile(tokensPath, secret string, stderr io.Writer) {
	path := openvmsKeyFile(tokensPath)
	if err := saveKeyFile(path, secret); err != nil {
		fmt.Fprintf(stderr, "could not save the new key to %s: %v\n", path, err)
		return
	}
	fmt.Fprintf(stderr, "The key in %s was updated.\n", path)
}

// openvmsKeyFile is where the OpenVMS key is kept on the host: next to the tokens file, readable by
// root only. It lets the operator read the key again instead of rotating it when it is lost.
func openvmsKeyFile(tokensPath string) string {
	if tokensPath == "" {
		tokensPath = tokensFile()
	}
	return filepath.Join(filepath.Dir(tokensPath), "openvms.apikey")
}

// saveKeyFile writes the key with mode 0600 through a temporary file, so a failure never leaves a
// half-written or world-readable key behind.
func saveKeyFile(path, secret string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".openvms.apikey.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // best effort; a successful rename already moved it
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(secret + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// runConnectionCommand creates the token OpenVMS uses to reach this agent, keeps its key in a private
// file and prints what to enter in OpenVMS. It never replaces an existing one: a second run points at
// `token rotate`.
func runConnectionCommand(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs, file := newConsoleFlags("connection", stderr)
	port := fs.Int("port", 7419, "port the agent listens on")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	secret, _, err := openTokens(*file).Create(openvmsTokenName, tokens.AllScopes, 0, now())
	if err != nil {
		fmt.Fprintln(stderr, "connection:", err)
		fmt.Fprintln(stderr, `If the "openvms" token exists, its key is in `+openvmsKeyFile(*file)+`; to replace it: openvms-agent token rotate openvms`)
		return 1
	}
	keyPath := openvmsKeyFile(*file)
	saveErr := saveKeyFile(keyPath, secret)
	if saveErr != nil {
		// The token exists now: the operator must still get the key.
		fmt.Fprintf(stderr, "could not save the key to %s: %v\n", keyPath, saveErr)
	}
	addrs := listLANAddresses()
	fmt.Fprintf(stdout, "OpenVMS agent connection\n")
	fmt.Fprintf(stdout, "  Agent version  %s\n", agent.Version)
	fmt.Fprintf(stdout, "  Key            %s\n", secret)
	if saveErr == nil {
		fmt.Fprintf(stdout, "  Key saved in   %s (readable by root only)\n", keyPath)
	}
	fmt.Fprintf(stdout, "  Addresses      this host answers on:\n")
	for _, a := range addrs {
		fmt.Fprintf(stdout, "                   http://%s\n", net.JoinHostPort(a, fmt.Sprint(*port)))
	}
	fmt.Fprintf(stdout, "\nNext steps, in OpenVMS\n")
	fmt.Fprintf(stdout, "  1. Servers > the server > \"Conectar agente\".\n")
	fmt.Fprintf(stdout, "  2. Address: the one OpenVMS reaches (a VPN address, or https://name if you publish the port through a tunnel).\n")
	fmt.Fprintf(stdout, "  3. Key: the one above. OpenVMS checks the address and the key before saving and shows what the key allows.\n")
	fmt.Fprintf(stdout, "\nThis host never calls OpenVMS: only port %d has to be reachable from OpenVMS.\n", *port)
	fmt.Fprintf(stdout, "Lost the key?  cat %s     New key?  openvms-agent token rotate openvms\n", keyPath)
	return 0
}

// listLANAddresses is replaced in tests.
var listLANAddresses = lanAddresses

// lanAddresses returns this host's non-loopback IPv4 addresses, or a placeholder when there is none.
func lanAddresses() []string {
	var out []string
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				out = append(out, ipnet.IP.String())
			}
		}
	}
	if len(out) == 0 {
		return []string{"<this-host>"}
	}
	return out
}
