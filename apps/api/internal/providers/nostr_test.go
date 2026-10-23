package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/coder/websocket"
)

// fakeRelay is a Nostr relay: mode "ok" accepts every event, "refuse" answers OK false, "limit" rate-limits,
// "silent" reads the event and never answers.
type fakeRelay struct {
	mu     sync.Mutex
	mode   string
	events []nostrEvent
	meta   string // kind-0 content served to REQ
	srv    *httptest.Server
}

func (f *fakeRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return
		}
		var msg []json.RawMessage
		if json.Unmarshal(raw, &msg) != nil || len(msg) < 2 {
			continue
		}
		var typ string
		_ = json.Unmarshal(msg[0], &typ)
		switch typ {
		case "REQ":
			var sub string
			_ = json.Unmarshal(msg[1], &sub)
			f.mu.Lock()
			meta := f.meta
			f.mu.Unlock()
			if meta != "" {
				ev, _ := json.Marshal([]any{"EVENT", sub, nostrEvent{Kind: 0, CreatedAt: 1, Content: meta}})
				_ = c.Write(ctx, websocket.MessageText, ev)
			}
			eose, _ := json.Marshal([]any{"EOSE", sub})
			_ = c.Write(ctx, websocket.MessageText, eose)
		case "EVENT":
			var ev nostrEvent
			_ = json.Unmarshal(msg[1], &ev)
			f.mu.Lock()
			f.events = append(f.events, ev)
			mode := f.mode
			f.mu.Unlock()
			var answer []any
			switch mode {
			case "silent":
				continue
			case "refuse":
				answer = []any{"OK", ev.ID, false, "blocked: you are banned from posting here"}
			case "limit":
				answer = []any{"OK", ev.ID, false, "rate-limited: slow down"}
			default:
				answer = []any{"OK", ev.ID, true, ""}
			}
			out, _ := json.Marshal(answer)
			_ = c.Write(ctx, websocket.MessageText, out)
		}
	}
}

func newFakeRelay(t *testing.T, mode string) *fakeRelay {
	t.Helper()
	f := &fakeRelay{mode: mode}
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelay) url() string { return "ws" + strings.TrimPrefix(f.srv.URL, "http") }

func useNostrClock(t *testing.T) {
	t.Helper()
	wait, now := nostrWait, nostrNow
	nostrWait = 500 * time.Millisecond
	nostrNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	t.Cleanup(func() { nostrWait, nostrNow = wait, now })
}

// A key from NIP-19's examples (never a real account).
const nostrTestHex = "67dea2ed018072d675f5415ecfaed7d2597555e202d85b3d65ea4e58d2d92ffa"

func TestNostrKeysAndBech32(t *testing.T) {
	nsec, _ := hex.DecodeString(nostrTestHex)
	enc, err := bech32Encode("nsec", nsec)
	if err != nil || enc != "nsec1vl029mgpspedva04g90vltkh6fvh240zqtv9k0t9af8935ke9laqsnlfe5" {
		t.Fatalf("nsec %q %v", enc, err)
	}
	a, err := nostrKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := nostrKey(nostrTestHex)
	if err != nil || hex.EncodeToString(a.Serialize()) != hex.EncodeToString(b.Serialize()) {
		t.Fatalf("hex and nsec differ: %v", err)
	}
	pub, _ := hex.DecodeString("7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e")
	if npub, _ := bech32Encode("npub", pub); npub != "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg" {
		t.Fatalf("npub %q", npub)
	}
	for _, bad := range []string{"npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg", "nsec1vl029mgpspedva04g90vltkh6fvh240zqtv9k0t9af8935ke9laqsnlfe6", "abc"} {
		if _, err := nostrKey(bad); err == nil {
			t.Errorf("%q was taken as a private key", bad)
		}
	}
}

func TestNostrSerializeMatchesNIP01(t *testing.T) {
	ev := nostrEvent{PubKey: "ab", CreatedAt: 5, Kind: 1, Tags: [][]string{{"t", "go"}}, Content: "a \"q\" \\ <b>&\n\t  é \x01"}
	got := string(nostrSerialize(ev))
	want := `[0,"ab",5,1,[["t","go"]],"a \"q\" \\ <b>&\n\t` + " " + ` é \u0001"]`
	if got != want {
		t.Fatalf("serialized\n%s\nwant\n%s", got, want)
	}
}

func TestNostrConnect(t *testing.T) {
	useNostrClock(t)
	r := newFakeRelay(t, "ok")
	r.meta = `{"name":"jane","display_name":"Jane Doe","picture":"https://example.com/a.png"}`
	e, _ := Get("nostr")
	if e.Status != StatusAvailable || e.Limits().MaxMedia != 0 {
		t.Fatalf("nostr is %q %+v", e.Status, e.Limits())
	}
	creds, prof, err := e.Connect(context.Background(), map[string]string{"private_key": nostrTestHex, "relays": r.url() + ", " + r.url() + "/"})
	if err != nil {
		t.Fatal(err)
	}
	if creds["private_key"] != nostrTestHex || creds["relays"] != r.url() || prof.Name != "Jane Doe" || !strings.HasPrefix(prof.Handle, "npub1") {
		t.Fatalf("creds %v profile %+v", creds, prof)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string(creds)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Connect(context.Background(), map[string]string{"private_key": nostrTestHex, "relays": "https://example.com"}); err == nil {
		t.Fatal("an https relay was taken")
	}
	r.srv.Close()
	if _, _, err := e.Connect(context.Background(), map[string]string{"private_key": nostrTestHex, "relays": r.url()}); !errors.Is(err, ErrRejected) {
		t.Fatalf("no relay reachable: %v", err)
	}
}

func TestNostrPublish(t *testing.T) {
	useNostrClock(t)
	good, bad := newFakeRelay(t, "ok"), newFakeRelay(t, "refuse")
	creds := Credentials{"private_key": nostrTestHex, "relays": bad.url() + ", " + good.url()}
	e, _ := Get("nostr")
	res, err := e.Publish(context.Background(), creds, PostContent{Text: "Hello #Nostr and #nostr"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ev := good.events[0]
	id := sha256.Sum256(nostrSerialize(ev))
	if res.ExternalID != ev.ID || ev.ID != hex.EncodeToString(id[:]) || !strings.HasPrefix(res.URL, nostrViewer+"note1") {
		t.Fatalf("result %+v event %+v", res, ev)
	}
	sig, _ := hex.DecodeString(ev.Sig)
	pub, _ := hex.DecodeString(ev.PubKey)
	s, err := schnorr.ParseSignature(sig)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := schnorr.ParsePubKey(pub)
	if err != nil || !s.Verify(id[:], pk) {
		t.Fatalf("the signature does not verify: %v", err)
	}
	if len(ev.Tags) != 1 || ev.Tags[0][0] != "t" || ev.Tags[0][1] != "nostr" || ev.CreatedAt != 1_700_000_000 {
		t.Fatalf("tags %v", ev.Tags)
	}

	// Replies: NIP-10 root and reply markers, and the author's p tag.
	r1, err := Nostr{}.Reply(context.Background(), creds, res.ExternalID, PostContent{Text: "two"}, nil)
	if err != nil || r1.ExternalID != res.ExternalID+":"+good.events[1].ID {
		t.Fatalf("reply %v %+v", err, r1)
	}
	if tags := good.events[1].Tags; len(tags) != 2 || tags[0][3] != "root" || tags[0][1] != res.ExternalID || tags[1][0] != "p" {
		t.Fatalf("reply tags %v", tags)
	}
	r2, err := Nostr{}.Reply(context.Background(), creds, r1.ExternalID, PostContent{Text: "three"}, nil)
	if err != nil || !strings.HasPrefix(r2.ExternalID, res.ExternalID+":") {
		t.Fatalf("second reply %v %+v", err, r2)
	}
	if tags := good.events[2].Tags; len(tags) != 3 || tags[0][1] != res.ExternalID || tags[1][1] != good.events[1].ID || tags[1][3] != "reply" {
		t.Fatalf("second reply tags %v", tags)
	}
}

func TestNostrFailures(t *testing.T) {
	useNostrClock(t)
	e, _ := Get("nostr")
	post := PostContent{Text: "Hello"}
	refuse, limit, silent := newFakeRelay(t, "refuse"), newFakeRelay(t, "limit"), newFakeRelay(t, "silent")
	down := newFakeRelay(t, "ok")
	down.srv.Close()

	_, err := e.Publish(context.Background(), Credentials{"private_key": nostrTestHex, "relays": refuse.url()}, post, nil)
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "banned") {
		t.Fatalf("refused: %v", err)
	}
	_, err = e.Publish(context.Background(), Credentials{"private_key": nostrTestHex, "relays": limit.url()}, post, nil)
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("rate limited should be retried: %v", err)
	}
	_, err = e.Publish(context.Background(), Credentials{"private_key": nostrTestHex, "relays": down.url()}, post, nil)
	if err == nil || errors.Is(err, ErrRejected) {
		t.Fatalf("no relay reachable should be retried: %v", err)
	}
	_, err = e.Publish(context.Background(), Credentials{"private_key": nostrTestHex, "relays": silent.url() + "," + refuse.url()}, post, nil)
	if !errors.Is(err, ErrRejected) || !strings.Contains(err.Error(), "may have published") {
		t.Fatalf("a lost answer: %v", err)
	}
	if _, err := e.Publish(context.Background(), Credentials{"relays": refuse.url()}, post, nil); !errors.Is(err, ErrReauth) {
		t.Fatalf("no key: %v", err)
	}
}

func TestNostrValidate(t *testing.T) {
	e, _ := Get("nostr")
	if err := e.Validate(PostContent{Text: strings.Repeat("a", 5000)}, nil); err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(PostContent{Text: "hi", Media: []MediaFile{{Name: "a.png", MimeType: "image/png"}}}, nil); err == nil {
		t.Fatal("media was taken")
	}
}
