package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Media struct {
	ID           string   `json:"id"`
	MimeType     string   `json:"mime_type"`
	SizeBytes    int64    `json:"size_bytes"`
	OriginalName string   `json:"original_name"`
	URL          string   `json:"url"`
	SHA256       string   `json:"sha256"`
	CreatedAt    string   `json:"created_at"`
	AltText      string   `json:"alt_text"`
	Width        int64    `json:"width"`
	Height       int64    `json:"height"`
	DurationMs   int64    `json:"duration_ms"`
	FolderID     string   `json:"folder_id"`
	Tags         []string `json:"tags"`
	Uses         int64    `json:"uses"`
}

// mediaSize is "1080x1350" or "-" when unknown.
func mediaSize(m Media) string {
	if m.Width == 0 || m.Height == 0 {
		return "-"
	}
	return fmt.Sprintf("%dx%d", m.Width, m.Height)
}

func mediaCommand() *Command {
	return &Command{
		Name: "media", Aliases: []string{"files"},
		Short: "The media library: upload, list, describe, download and delete files",
		Subs: []*Command{
			leaf("list", "", "List the media library, newest first", 0, 0, func(fs *flag.FlagSet) runFunc {
				q := fs.String("q", "", "part of the file `name`")
				kind := fs.String("kind", "", "image, video or audio")
				used := fs.String("used", "", "used (posts carry it) or unused")
				folder := fs.String("folder", "", "a folder `id` (files directly in it) or root (the top level)")
				tag := fs.String("tag", "", "files carrying this `tag`")
				deleted := fs.Bool("deleted", false, "list the files in Deleted instead")
				duplicates := fs.Bool("duplicates", false, "only files with an identical copy, grouped")
				sort := fs.String("sort", "", "created_desc (default), created_asc, size_desc or name_asc")
				limit := fs.Int("limit", 0, "page size (default 60, max 200)")
				offset := fs.Int("offset", 0, "start at this position (next_offset of the previous page)")
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					v := url.Values{}
					flags := map[string]string{"q": *q, "kind": *kind, "used": *used, "folder": *folder, "tag": *tag, "sort": *sort}
					if *deleted {
						flags["deleted"] = "true"
					}
					if *duplicates {
						flags["duplicates"] = "true"
					}
					for k, val := range flags {
						if val != "" {
							v.Set(k, val)
						}
					}
					if *limit > 0 {
						v.Set("limit", strconv.Itoa(*limit))
					}
					if *offset > 0 {
						v.Set("offset", strconv.Itoa(*offset))
					}
					var page struct {
						Data       []Media
						NextOffset *int `json:"next_offset"`
					}
					raw, err := cl.Get(c, "/v1/media", v, &page)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "NAME", "TYPE", "SIZE", "PIXELS", "POSTS", "TAGS")
					for _, m := range page.Data {
						tags := strings.Join(m.Tags, ",")
						if tags == "" {
							tags = "-"
						}
						t.row(m.ID, m.OriginalName, m.MimeType, humanBytes(m.SizeBytes), mediaSize(m), fmt.Sprint(m.Uses), tags)
					}
					t.flush("No files yet. Upload one with: staqly media upload <file>")
					if page.NextOffset != nil {
						fmt.Fprintf(c.Stderr, "More: staqly media list --offset %d\n", *page.NextOffset)
					}
					return nil
				}
			}, "staqly media list", "staqly media list --kind video --used unused", "staqly media list --folder root --tag hero", "staqly media list --q launch --json"),

			leaf("mv", "<id>...", "Move files into a folder (--folder root for the top level)", 1, -1, func(fs *flag.FlagSet) runFunc {
				folder := fs.String("folder", "", "the folder `id`, or root for the top level")
				return func(c *Ctx, args []string) error {
					if *folder == "" {
						return usageErrf("pass --folder <id> or --folder root")
					}
					to := *folder
					if to == "root" {
						to = ""
					}
					return mediaBulk(c, map[string]any{"ids": args, "folder_id": to}, "Moved")
				}
			}, "staqly media mv med_oalirbz67y4m4cqxq5dw --folder mfd_k2s7pbk7igv6", "staqly media mv med_a med_b --folder root"),

			leaf("tag", "<id>...", "Add or remove tags on files", 1, -1, func(fs *flag.FlagSet) runFunc {
				add := fs.String("add", "", "comma-separated `tags` to add")
				remove := fs.String("remove", "", "comma-separated `tags` to remove")
				return func(c *Ctx, args []string) error {
					if *add == "" && *remove == "" {
						return usageErrf("pass --add or --remove")
					}
					return mediaBulk(c, map[string]any{"ids": args, "add_tags": splitList(*add), "remove_tags": splitList(*remove)}, "Tagged")
				}
			}, "staqly media tag med_oalirbz67y4m4cqxq5dw --add hero,summer-sale", "staqly media tag med_a med_b --remove draft"),

			mediaFoldersCommand(),

			leaf("update", "<id>", "Rename a file or set its alt text", 1, 1, func(fs *flag.FlagSet) runFunc {
				name := fs.String("name", "", "new file `name`")
				alt := fs.String("alt", "", "alt text: what the picture shows, for people who can't see it")
				return func(c *Ctx, args []string) error {
					body := map[string]any{}
					fs.Visit(func(f *flag.Flag) {
						switch f.Name {
						case "name":
							body["name"] = *name
						case "alt":
							body["alt_text"] = *alt
						}
					})
					if len(body) == 0 {
						return usageErrf("pass --name or --alt")
					}
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var m Media
					raw, err := cl.Patch(c, "/v1/media/"+url.PathEscape(args[0]), body, &m)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Updated %s (%s)\n", c.paint(bold, m.ID), m.OriginalName)
					return nil
				}
			}, `staqly media update med_oalirbz67y4m4cqxq5dw --alt "A runner at the finish line"`, "staqly media update med_oalirbz67y4m4cqxq5dw --name cover.png"),

			leaf("usage", "<id>", "Which posts use a file", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var out struct {
						Data []struct {
							PostID    string  `json:"post_id"`
							Title     string  `json:"title"`
							Status    string  `json:"status"`
							PublishAt *string `json:"publish_at"`
						}
					}
					raw, err := cl.Get(c, "/v1/media/"+url.PathEscape(args[0])+"/usage", nil, &out)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("POST", "STATUS", "PUBLISH AT", "TITLE")
					for _, u := range out.Data {
						at := "-"
						if u.PublishAt != nil {
							at = *u.PublishAt
						}
						t.row(u.PostID, u.Status, at, u.Title)
					}
					t.flush("No post uses this file.")
					return nil
				}
			}, "staqly media usage med_oalirbz67y4m4cqxq5dw"),

			leaf("upload", "<file>", "Upload a file; attach it with `posts create --media <id>` or `posts update <id> --media <id>`", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					f, err := os.Open(args[0])
					if err != nil {
						return fmt.Errorf("open file: %w", err)
					}
					defer f.Close()
					pr, pw := io.Pipe()
					mw := multipart.NewWriter(pw)
					go func() {
						err := func() error {
							part, err := mw.CreateFormFile("file", filepath.Base(args[0]))
							if err != nil {
								return err
							}
							if _, err := io.Copy(part, f); err != nil {
								return err
							}
							return mw.Close()
						}()
						pw.CloseWithError(err)
					}()
					var m Media
					raw, err := cl.call(c, Request{Method: http.MethodPost, Path: "/v1/media", Body: io.Reader(pr), ContentType: mw.FormDataContentType()}, &m)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Uploaded %s (%s, %s)\n", c.paint(bold, m.ID), m.MimeType, humanBytes(m.SizeBytes))
					fmt.Fprintf(c.Out, "Attach it to a post with: staqly posts create <collection> --media %s or staqly posts update <post-id> --media %s\n", m.ID, m.ID)
					return nil
				}
			}, "staqly media upload cover.png", "staqly media upload report.pdf --json | jq -r .id"),

			leaf("get", "<id>", "Download a file (or a thumbnail with --width)", 1, 1, func(fs *flag.FlagSet) runFunc {
				output := fs.String("output", "", "write to this `file` ('-' = stdout; default: stdout when piped, else the original file name)")
				fs.StringVar(output, "o", "", "shorthand for --output")
				width := fs.Int("width", 0, "thumbnail width for images: 160, 320, 640 or 1280")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{}
					if *width > 0 {
						q.Set("w", strconv.Itoa(*width))
					}
					return download(c, cl, "/v1/media/"+url.PathEscape(args[0]), q, *output, args[0], true)
				}
			}, "staqly media get med_oalirbz67y4m4cqxq5dw", "staqly media get med_oalirbz67y4m4cqxq5dw --width 320 -o thumb.jpg",
				"staqly media get med_oalirbz67y4m4cqxq5dw -o - | file -"),

			leaf("delete", "<id>", "Move a file to Deleted for 30 days; --permanent deletes it for good (API keys: posts:delete)", 1, 1, func(fs *flag.FlagSet) runFunc {
				permanent := fs.Bool("permanent", false, "delete it for good now; posts that use it lose it")
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					q := url.Values{}
					if *permanent {
						q.Set("permanent", "true")
					}
					if err := cl.Delete(c, "/v1/media/"+url.PathEscape(args[0]), q); err != nil {
						return err
					}
					if c.JSON {
						return c.printJSON(map[string]any{"deleted": true, "id": args[0], "permanent": *permanent})
					}
					if *permanent {
						fmt.Fprintf(c.Out, "Deleted %s for good\n", args[0])
					} else {
						fmt.Fprintf(c.Out, "Moved %s to Deleted (staqly media restore %s brings it back within 30 days)\n", args[0], args[0])
					}
					return nil
				}
			}, "staqly media delete med_oalirbz67y4m4cqxq5dw", "staqly media delete med_oalirbz67y4m4cqxq5dw --permanent"),
			leaf("restore", "<id>", "Take a file out of Deleted (API keys: posts:delete)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var m Media
					raw, err := cl.Post(c, "/v1/media/"+url.PathEscape(args[0])+"/restore", nil, &m)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Restored %s (%s)\n", m.ID, m.OriginalName)
					return nil
				}
			}, "staqly media restore med_oalirbz67y4m4cqxq5dw"),
			leaf("keep", "<id>", "Keep this file of identical copies: posts use it, the copies move to Deleted", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var out struct {
						Deleted []string `json:"deleted"`
					}
					raw, err := cl.Post(c, "/v1/media/"+url.PathEscape(args[0])+"/keep", nil, &out)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					fmt.Fprintf(c.Out, "Kept %s; moved %s to Deleted\n", args[0], plural(len(out.Deleted), "copy", "copies"))
					return nil
				}
			}, "staqly media list --duplicates", "staqly media keep med_oalirbz67y4m4cqxq5dw"),
		},
	}
}

// download streams a GET response to output:
//   - "-": stdout
//   - a path: that file (overwritten)
//   - "": stdout, unless the content is binary and stdout is a terminal; then
//     a new file in the current directory named after the server's
//     Content-Disposition file name (or fallback), never overwriting one.
//
// Text written to a terminal is sanitized line by line. When a file is
// written, a confirmation goes to stderr (or a JSON summary to stdout).
func download(c *Ctx, cl *Client, path string, q url.Values, output, fallback string, binary bool) error {
	resp, err := cl.Stream(c, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ctype := resp.Header.Get("Content-Type")
	toStdout := output == "-" || (output == "" && (!binary || !c.Env.StdoutTTY))
	if toStdout {
		if binary && c.Env.StdoutTTY {
			return usageErrf("refusing to write binary data to a terminal; pass --output FILE")
		}
		if c.Env.StdoutTTY {
			return copyClean(c.Out, resp.Body)
		}
		_, err := io.Copy(c.Out, resp.Body)
		return err
	}
	exclusive := false
	if output == "" {
		output, exclusive = suggestedName(resp.Header.Get("Content-Disposition"), fallback), true
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(output, flags, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return &exitErr{code: ExitError, msg: fmt.Sprintf("%s already exists; pass --output to choose a file", output)}
	}
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(output)
		return fmt.Errorf("write %s: %w", output, err)
	}
	if c.JSON {
		return c.printJSON(map[string]any{"path": output, "bytes": n, "content_type": ctype})
	}
	fmt.Fprintf(c.Stderr, "Saved %s (%s)\n", output, humanBytes(n))
	return nil
}

// copyClean copies text to a terminal, stripping control sequences.
func copyClean(w io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if _, err := fmt.Fprintln(w, clean(sc.Text())); err != nil {
			return err
		}
	}
	return sc.Err()
}

// suggestedName is a safe local file name from Content-Disposition.
func suggestedName(disposition, fallback string) string {
	name := ""
	if _, params, err := mime.ParseMediaType(disposition); err == nil {
		name = params["filename"]
	}
	name = cleanLine(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") || name == "/" {
		name = filepath.Base(fallback)
	}
	return name
}

// mediaBulk moves or tags several files at once and reports what changed.
func mediaBulk(c *Ctx, body map[string]any, verb string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var out struct {
		Updated []string `json:"updated"`
		Missing []string `json:"missing"`
	}
	raw, err := cl.Post(c, "/v1/media/bulk", body, &out)
	if err != nil || c.JSON {
		return jsonOr(c, raw, err)
	}
	fmt.Fprintf(c.Out, "%s %s\n", verb, plural(len(out.Updated), "file", "files"))
	if len(out.Missing) > 0 {
		fmt.Fprintf(c.Stderr, "Not found: %s\n", strings.Join(out.Missing, ", "))
	}
	return nil
}

func splitList(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

type mediaFolder struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
	Name     string `json:"name"`
	Files    int64  `json:"files"`
}

// mediaFoldersCommand: the library's virtual folders (they only organize; deleting one never deletes files).
func mediaFoldersCommand() *Command {
	return &Command{
		Name: "folders", Short: "The media library's folders: list, create, rename, move, delete",
		Subs: []*Command{
			leaf("list", "", "List folders as a tree, with file counts and tags", 0, 0, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, _ []string) error {
					cl, err := c.Client()
					if err != nil {
						return err
					}
					var tree struct {
						Data      []mediaFolder `json:"data"`
						RootFiles int64         `json:"root_files"`
						Total     int64         `json:"total_files"`
						Tags      []struct {
							Tag   string `json:"tag"`
							Files int64  `json:"files"`
						} `json:"tags"`
					}
					raw, err := cl.Get(c, "/v1/media-folders", nil, &tree)
					if err != nil || c.JSON {
						return jsonOr(c, raw, err)
					}
					t := c.table("ID", "FOLDER", "FILES")
					t.row("root", "(top level)", fmt.Sprint(tree.RootFiles))
					var walk func(parent string, depth int)
					walk = func(parent string, depth int) {
						for _, f := range tree.Data {
							if f.ParentID == parent {
								t.row(f.ID, strings.Repeat("  ", depth)+f.Name, fmt.Sprint(f.Files))
								walk(f.ID, depth+1)
							}
						}
					}
					walk("", 0)
					t.flush("")
					if len(tree.Tags) > 0 {
						parts := make([]string, 0, len(tree.Tags))
						for _, tg := range tree.Tags {
							parts = append(parts, fmt.Sprintf("%s (%d)", tg.Tag, tg.Files))
						}
						fmt.Fprintf(c.Out, "Tags: %s\n", strings.Join(parts, ", "))
					}
					return nil
				}
			}, "staqly media folders list"),
			leaf("create", "<name>", "Create a folder (--in for a subfolder)", 1, 1, func(fs *flag.FlagSet) runFunc {
				in := fs.String("in", "", "the parent folder `id` (default: the top level)")
				return func(c *Ctx, args []string) error {
					return mediaFolderWrite(c, "POST", "/v1/media-folders", map[string]any{"name": args[0], "parent_id": *in}, "Created")
				}
			}, `staqly media folders create "Clients"`, `staqly media folders create "ACME" --in mfd_k2s7pbk7igv6`),
			leaf("rename", "<id> <name>", "Rename a folder", 2, 2, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return mediaFolderWrite(c, "PATCH", "/v1/media-folders/"+url.PathEscape(args[0]), map[string]any{"name": args[1]}, "Renamed")
				}
			}, `staqly media folders rename mfd_k2s7pbk7igv6 "ACME 2027"`),
			leaf("move", "<id>", "Move a folder under another one (--to root for the top level)", 1, 1, func(fs *flag.FlagSet) runFunc {
				to := fs.String("to", "", "the new parent folder `id`, or root")
				return func(c *Ctx, args []string) error {
					if *to == "" {
						return usageErrf("pass --to <id> or --to root")
					}
					parent := *to
					if parent == "root" {
						parent = ""
					}
					return mediaFolderWrite(c, "PATCH", "/v1/media-folders/"+url.PathEscape(args[0]), map[string]any{"parent_id": parent}, "Moved")
				}
			}, "staqly media folders move mfd_k2s7pbk7igv6 --to root"),
			leaf("delete", "<id>", "Delete a folder; its files and subfolders move up (no file is deleted)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/media-folders/"+url.PathEscape(args[0]), "folder", args[0])
				}
			}, "staqly media folders delete mfd_k2s7pbk7igv6"),
		},
	}
}

func mediaFolderWrite(c *Ctx, method, path string, body map[string]any, verb string) error {
	cl, err := c.Client()
	if err != nil {
		return err
	}
	var f mediaFolder
	var raw json.RawMessage
	if method == "POST" {
		raw, err = cl.Post(c, path, body, &f)
	} else {
		raw, err = cl.Patch(c, path, body, &f)
	}
	if err != nil || c.JSON {
		return jsonOr(c, raw, err)
	}
	fmt.Fprintf(c.Out, "%s folder %s (%s)\n", verb, c.paint(bold, f.ID), f.Name)
	return nil
}
