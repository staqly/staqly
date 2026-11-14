package cli

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

func completionCommand() *Command {
	cmd := leaf("completion", "<bash|zsh|fish>", "Print a shell completion script", 1, 1, func(fs *flag.FlagSet) runFunc {
		return func(c *Ctx, args []string) error {
			root := link(rootCommand())
			switch args[0] {
			case "bash":
				writeBash(c.Out, root)
			case "zsh":
				fmt.Fprintln(c.Out, "#compdef staqly")
				fmt.Fprintln(c.Out, "autoload -U +X bashcompinit && bashcompinit")
				writeBash(c.Out, root)
			case "fish":
				writeFish(c.Out, root)
			default:
				return usageErrf("unknown shell %q; use bash, zsh or fish", args[0])
			}
			return nil
		}
	},
		"source <(staqly completion bash)          # add to ~/.bashrc",
		"source <(staqly completion zsh)           # add to ~/.zshrc",
		"staqly completion fish > ~/.config/fish/completions/staqly.fish")
	return cmd
}

func visibleSubs(c *Command) []*Command {
	var out []*Command
	for _, s := range c.Subs {
		if !s.Hidden {
			out = append(out, s)
		}
	}
	return out
}

func subNames(c *Command) string {
	var n []string
	for _, s := range visibleSubs(c) {
		n = append(n, s.Name)
	}
	return strings.Join(n, " ")
}

// commandFlags lists a command's flags (including globals) as --name.
func commandFlags(c *Command) []string {
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	g := addGlobals(fs)
	if c.Setup != nil {
		c.Setup(fs)
	}
	addURLAlias(fs, g)
	var out []string
	fs.VisitAll(func(f *flag.Flag) {
		if len(f.Name) > 1 {
			out = append(out, "--"+f.Name)
		}
	})
	sort.Strings(out)
	return out
}

func writeBash(w io.Writer, root *Command) {
	fmt.Fprintln(w, `# bash completion for staqly
_staqly() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  local c1="${COMP_WORDS[1]}" c2="${COMP_WORDS[2]}"
  if [[ "$cur" == -* ]]; then
    local flags
    case "$c1 $c2" in`)
	for _, s := range visibleSubs(root) {
		if len(s.Subs) == 0 {
			fmt.Fprintf(w, "      \"%s \"*) flags=%q ;;\n", s.Name, strings.Join(commandFlags(s), " "))
			continue
		}
		for _, ss := range visibleSubs(s) {
			fmt.Fprintf(w, "      \"%s %s\") flags=%q ;;\n", s.Name, ss.Name, strings.Join(commandFlags(ss), " "))
		}
	}
	fmt.Fprintf(w, "      *) flags=%q ;;\n", strings.Join(commandFlags(&Command{}), " "))
	fmt.Fprintln(w, `    esac
    COMPREPLY=($(compgen -W "$flags" -- "$cur"))
    return
  fi
  if [[ $COMP_CWORD -eq 1 ]]; then
    COMPREPLY=($(compgen -W "`+subNames(root)+`" -- "$cur"))
    return
  fi
  if [[ $COMP_CWORD -eq 2 ]]; then
    case "$c1" in`)
	for _, s := range visibleSubs(root) {
		if len(s.Subs) > 0 {
			fmt.Fprintf(w, "      %s) COMPREPLY=($(compgen -W %q -- \"$cur\")) ;;\n", s.Name, subNames(s))
		}
	}
	fmt.Fprintln(w, `      completion) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")) ;;
    esac
    return
  fi
  if [[ $COMP_CWORD -eq 3 && "$c1 $c2" == "mcp config" ]]; then
    COMPREPLY=($(compgen -W "claude codex opencode cursor" -- "$cur"))
  fi
}
complete -F _staqly staqly`)
}

func writeFish(w io.Writer, root *Command) {
	fmt.Fprintln(w, "# fish completion for staqly")
	fmt.Fprintln(w, "complete -c staqly -f")
	var tops []string
	for _, s := range visibleSubs(root) {
		tops = append(tops, s.Name)
	}
	for _, s := range visibleSubs(root) {
		fmt.Fprintf(w, "complete -c staqly -n \"not __fish_seen_subcommand_from %s\" -a %s -d %q\n", strings.Join(tops, " "), s.Name, s.Short)
		for _, ss := range visibleSubs(s) {
			fmt.Fprintf(w, "complete -c staqly -n \"__fish_seen_subcommand_from %s; and not __fish_seen_subcommand_from %s\" -a %s -d %q\n",
				s.Name, subNames(s), ss.Name, ss.Short)
		}
	}
	fmt.Fprintln(w, "complete -c staqly -n \"__fish_seen_subcommand_from completion\" -a \"bash zsh fish\"")
	fmt.Fprintln(w, "complete -c staqly -n \"__fish_seen_subcommand_from config\" -a \"claude codex opencode cursor\"")
	seen := map[string]bool{}
	for _, s := range visibleSubs(root) {
		leaves := []*Command{s}
		if len(s.Subs) > 0 {
			leaves = visibleSubs(s)
		}
		for _, l := range leaves {
			for _, f := range commandFlags(l) {
				name := strings.TrimPrefix(f, "--")
				if !seen[name] {
					seen[name] = true
					fmt.Fprintf(w, "complete -c staqly -l %s\n", name)
				}
			}
		}
	}
}
