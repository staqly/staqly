package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

// mcpSnippet is one client configuration; it never contains the API key
// itself, only a reference to the STAQLY_API_KEY environment variable.
type mcpSnippet struct {
	Client    string `json:"client"`
	Transport string `json:"transport"`
	Command   string `json:"command,omitempty"`
	File      string `json:"file,omitempty"`
	Format    string `json:"format,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
	Note      string `json:"note,omitempty"`
}

func mcpCommand() *Command {
	cfg := leaf("config", "<claude|codex|opencode|cursor|windsurf>", "Print the MCP configuration for an agent client", 1, 1, func(fs *flag.FlagSet) runFunc {
		stdio := fs.Bool("stdio", false, "use the STDIO server (docker exec ... mcp-stdio) instead of HTTP")
		container := fs.String("container", "staqly", "API container name for --stdio")
		snippetOnly := fs.Bool("snippet", false, "print only the config file snippet (for redirecting into a file)")
		return func(c *Ctx, args []string) error {
			cl, err := c.Client()
			if err != nil {
				return err
			}
			// Over HTTP every server wants a key (local mode: its access token); STDIO opens the database
			// itself, so only a cloud key matters there.
			local := cl.Key == "" || strings.HasPrefix(cl.Key, "stk_local_")
			withKey := !*stdio || !local
			s, err := buildMCPSnippet(args[0], cl.BaseURL+"/mcp", withKey, *stdio, *container)
			if err != nil {
				return err
			}
			switch {
			case c.JSON:
				return c.printJSON(s)
			case *snippetOnly:
				if s.Snippet == "" {
					return &exitErr{code: ExitError, msg: "no key-free config file snippet for this setup; run the command instead:\n  " + s.Command}
				}
				fmt.Fprintln(c.Out, s.Snippet)
				return nil
			}
			if withKey && local {
				fmt.Fprintln(c.Out, "# The local access token is read from the environment; export it first")
				fmt.Fprintln(c.Out, "# (Docker: docker exec staqly server token):")
				fmt.Fprintln(c.Out, "export STAQLY_API_KEY=$(staqly serve token)")
				fmt.Fprintln(c.Out)
			} else if withKey {
				fmt.Fprintln(c.Out, "# The key is read from the environment; export it first:")
				fmt.Fprintln(c.Out, "export STAQLY_API_KEY=stk_...")
				fmt.Fprintln(c.Out)
			}
			if s.Command != "" {
				fmt.Fprintf(c.Out, "# Add it with the %s CLI:\n%s\n\n", s.Client, s.Command)
			}
			if s.Snippet != "" {
				fmt.Fprintf(c.Out, "# Or put this in %s:\n%s\n", s.File, s.Snippet)
			}
			if s.Note != "" {
				fmt.Fprintf(c.Out, "\n# %s\n", s.Note)
			}
			return nil
		}
	},
		"staqly mcp config claude",
		"staqly mcp config codex --profile work",
		"staqly mcp config opencode --stdio",
		"staqly mcp config cursor --snippet > .cursor/mcp.json",
		"staqly mcp config windsurf")
	cfg.Long = `Print the MCP configuration for Claude Code, Codex, OpenCode, Cursor or
Windsurf,
pointing at the current profile's server (<url>/mcp). Over HTTP the snippet
references $STAQLY_API_KEY: an API key, or a local server's access token
(staqly serve token); the key itself is never printed. With --stdio a local
server needs no key. See https://docs.staqly.ai/mcp.`
	return &Command{Name: "mcp", Short: "MCP setup helpers for agent clients", Subs: []*Command{cfg}}
}

func buildMCPSnippet(client, mcpURL string, withKey, stdio bool, container string) (mcpSnippet, error) {
	s := mcpSnippet{Client: client, Transport: "http"}
	if stdio {
		s.Transport = "stdio"
	}
	dockerArgs := []string{"exec", "-i"}
	if withKey {
		dockerArgs = append(dockerArgs, "-e", "STAQLY_API_KEY")
	}
	dockerArgs = append(dockerArgs, container, "mcp-stdio")
	dockerCmd := "docker " + strings.Join(dockerArgs, " ")
	if stdio {
		s.Note = "STDIO runs mcp-stdio inside the API container so it shares the database (use -i, never -t)."
	}
	switch client {
	case "claude", "claude-code":
		s.Client, s.File, s.Format = "claude", ".mcp.json (project) or ~/.claude.json", "json"
		server := map[string]any{"type": "http", "url": mcpURL}
		switch {
		case stdio && withKey:
			s.Command = "claude mcp add staqly -e STAQLY_API_KEY=$STAQLY_API_KEY -- " + dockerCmd
			server = map[string]any{"command": "docker", "args": dockerArgs, "env": map[string]string{"STAQLY_API_KEY": "${STAQLY_API_KEY}"}}
		case stdio:
			s.Command = "claude mcp add staqly -- " + dockerCmd
			server = map[string]any{"command": "docker", "args": dockerArgs}
		case withKey:
			s.Command = fmt.Sprintf("claude mcp add --transport http staqly %s --header \"Authorization: Bearer $STAQLY_API_KEY\"", mcpURL)
			server["headers"] = map[string]string{"Authorization": "Bearer ${STAQLY_API_KEY}"}
		default:
			s.Command = "claude mcp add --transport http staqly " + mcpURL
		}
		s.Snippet = mustIndent(map[string]any{"mcpServers": map[string]any{"staqly": server}})
	case "codex":
		s.File, s.Format = "~/.codex/config.toml", "toml"
		var b strings.Builder
		b.WriteString("[mcp_servers.staqly]\n")
		switch {
		case stdio && withKey:
			// `codex mcp add --env` stores the key's value in a
			// [mcp_servers.staqly.env] table, so there is no key-free TOML to
			// print: only the command, which reads $STAQLY_API_KEY from the shell.
			s.Command = "codex mcp add staqly --env STAQLY_API_KEY=$STAQLY_API_KEY -- " + dockerCmd
			s.File, s.Format, s.Snippet = "", "", ""
			s.Note = "codex writes the key's value into ~/.codex/config.toml ([mcp_servers.staqly.env]); keep that file private."
		case stdio:
			s.Command = "codex mcp add staqly -- " + dockerCmd
			quoted := make([]string, len(dockerArgs))
			for i, a := range dockerArgs {
				quoted[i] = fmt.Sprintf("%q", a)
			}
			fmt.Fprintf(&b, "command = \"docker\"\nargs = [%s]", strings.Join(quoted, ", "))
		case withKey:
			s.Command = fmt.Sprintf("codex mcp add staqly --url %s --bearer-token-env-var STAQLY_API_KEY", mcpURL)
			fmt.Fprintf(&b, "url = %q\nbearer_token_env_var = \"STAQLY_API_KEY\"", mcpURL)
		default:
			s.Command = "codex mcp add staqly --url " + mcpURL
			fmt.Fprintf(&b, "url = %q", mcpURL)
		}
		if s.Format != "" {
			s.Snippet = b.String()
		}
	case "opencode":
		s.File, s.Format = "opencode.json", "json"
		// OpenCode 2: staqly's tools called directly (not through code mode); await_decision may wait minutes.
		opts := func(m map[string]any) map[string]any {
			m["codemode"], m["timeout"] = false, map[string]int{"execution": 200000}
			return m
		}
		server := opts(map[string]any{"type": "remote", "url": mcpURL})
		if stdio {
			server = opts(map[string]any{"type": "local", "command": append([]string{"docker"}, dockerArgs...)})
			if withKey {
				server["environment"] = map[string]string{"STAQLY_API_KEY": "{env:STAQLY_API_KEY}"}
			}
		} else if withKey {
			server["headers"] = map[string]string{"Authorization": "Bearer {env:STAQLY_API_KEY}"}
		}
		s.Snippet = mustIndent(map[string]any{"$schema": "https://opencode.ai/config.json", "mcp": map[string]any{"servers": map[string]any{"staqly": server}}})
		s.Command = ""
		if s.Note == "" {
			s.Note = "Check with: opencode mcp list"
		}
	case "cursor":
		s.File, s.Format = ".cursor/mcp.json (project) or ~/.cursor/mcp.json", "json"
		server := map[string]any{"url": mcpURL}
		if stdio {
			// https://cursor.com/docs/context/mcp lists "type" as required for STDIO.
			server = map[string]any{"type": "stdio", "command": "docker", "args": dockerArgs}
			if withKey {
				server["env"] = map[string]string{"STAQLY_API_KEY": "${env:STAQLY_API_KEY}"}
			}
		} else if withKey {
			server["headers"] = map[string]string{"Authorization": "Bearer ${env:STAQLY_API_KEY}"}
		}
		s.Snippet = mustIndent(map[string]any{"mcpServers": map[string]any{"staqly": server}})
	case "windsurf":
		s.File, s.Format = "~/.codeium/windsurf/mcp_config.json", "json"
		server := map[string]any{"serverUrl": mcpURL}
		if stdio {
			server = map[string]any{"command": "docker", "args": dockerArgs}
			if withKey {
				server["env"] = map[string]string{"STAQLY_API_KEY": "${env:STAQLY_API_KEY}"}
			}
		} else if withKey {
			server["headers"] = map[string]string{"Authorization": "Bearer ${env:STAQLY_API_KEY}"}
		}
		s.Snippet = mustIndent(map[string]any{"mcpServers": map[string]any{"staqly": server}})
		s.Note = "Or open Windsurf Settings → Cascade → MCP servers → View raw config, paste it and press Refresh."
	default:
		return s, usageErrf("unknown client %q; use claude, codex, opencode, cursor or windsurf", client)
	}
	return s, nil
}

func mustIndent(v any) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}
