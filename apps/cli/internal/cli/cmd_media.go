package cli

import (
	"bufio"
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
	ID           string `json:"id"`
	MimeType     string `json:"mime_type"`
	SizeBytes    int64  `json:"size_bytes"`
	OriginalName string `json:"original_name"`
	URL          string `json:"url"`
	SHA256       string `json:"sha256"`
	CreatedAt    string `json:"created_at"`
}

func mediaCommand() *Command {
	return &Command{
		Name: "media", Aliases: []string{"files"},
		Short: "Upload, download and delete media files",
		Subs: []*Command{
			leaf("upload", "<file>", "Upload a file; attach it with `items update <id> --media-id <media-id>`", 1, 1, func(fs *flag.FlagSet) runFunc {
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
					fmt.Fprintf(c.Out, "Attach it to an item with: staqly items update <item-id> --media-id %s\n", m.ID)
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

			leaf("delete", "<id>", "Delete a file (items that use it lose their media)", 1, 1, func(fs *flag.FlagSet) runFunc {
				return func(c *Ctx, args []string) error {
					return simpleDelete(c, "/v1/media/"+url.PathEscape(args[0]), "media", args[0])
				}
			}, "staqly media delete med_oalirbz67y4m4cqxq5dw"),
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
