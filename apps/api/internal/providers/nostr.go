package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/coder/websocket"
)

// Nostr: the person pastes their private key (nsec... or hex) and picks relays; staqly signs a text note (kind 1,
// NIP-01, a BIP-340 Schnorr signature over secp256k1) and sends it to every relay over a websocket. A post is out
// when at least one relay answers OK. The post's thread goes out as replies (NIP-10 e and p tags).
//
// Pictures need an upload server of the person's (NIP-96 / Blossom), which staqly does not ask for yet, and staqly's
// own media links expire: Nostr channels take text only.
type Nostr struct{ Base }

var (
	// nostrWait is how long one relay may take to connect and answer.
	nostrWait = 10 * time.Second
	// nostrDefaultRelays are well-known public relays, used when the person names none.
	nostrDefaultRelays = "wss://relay.damus.io, wss://nos.lol, wss://relay.primal.net"
	// nostrViewer shows a note or a profile on the web.
	nostrViewer = "https://njump.me/"
	// nostrNow is the clock notes are stamped with (tests fix it).
	nostrNow = time.Now
)

func init() {
	Register(Nostr{NewBase("nostr", "Nostr", AuthToken, Limits{MaxChars: 0, MaxMedia: 0})}, StatusAvailable)
}

func (Nostr) CredentialFields() []Field {
	return []Field{
		{Key: "private_key", Label: "Private key", Kind: "secret", Required: true, Placeholder: "nsec1...",
			Help: "Your nsec key (or its 64-character hex form), from your Nostr app's key settings. Staqly keeps it sealed and signs your notes with it."},
		{Key: "relays", Label: "Relays", Kind: "list", MaxItems: 20, Default: nostrDefaultRelays, Placeholder: "wss://relay.damus.io",
			Help: "Where your notes are sent; use the relays your Nostr app writes to."},
	}
}

// nostrKey reads an nsec... or hex private key.
func nostrKey(v string) (*btcec.PrivateKey, error) {
	v = strings.TrimSpace(v)
	var raw []byte
	var err error
	if strings.HasPrefix(strings.ToLower(v), "nsec1") {
		var hrp string
		hrp, raw, err = bech32Decode(v)
		if err == nil && hrp != "nsec" {
			err = errors.New("not an nsec key")
		}
	} else {
		raw, err = hex.DecodeString(v)
	}
	if err != nil || len(raw) != 32 {
		return nil, invalid("private_key", "paste your private key: nsec1... or 64 hex characters (not the npub, which is public)")
	}
	key, _ := btcec.PrivKeyFromBytes(raw)
	if key.Key.IsZero() {
		return nil, invalid("private_key", "this private key is not valid")
	}
	return key, nil
}

// nostrRelays reads the relay list: wss:// addresses (ws:// only for a relay on a private network).
func nostrRelays(v string) ([]string, error) {
	if strings.TrimSpace(v) == "" {
		v = nostrDefaultRelays
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range ListValues(strings.ReplaceAll(v, "\n", ",")) {
		u, err := url.Parse(r)
		if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
			return nil, invalid("relays", r+" is not a relay address; relays start with wss://")
		}
		s := strings.TrimRight(u.String(), "/")
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) > 20 {
		return nil, invalid("relays", "at most 20 relays")
	}
	return out, nil
}

func (Nostr) Connect(ctx context.Context, f map[string]string) (Credentials, Profile, error) {
	key, err := nostrKey(f["private_key"])
	if err != nil {
		return nil, Profile{}, err
	}
	relays, err := nostrRelays(f["relays"])
	if err != nil {
		return nil, Profile{}, err
	}
	pub := schnorr.SerializePubKey(key.PubKey())
	npub, _ := bech32Encode("npub", pub)
	meta, reached := nostrProfile(ctx, relays, hex.EncodeToString(pub))
	if reached == 0 {
		return nil, Profile{}, &Failure{Kind: ErrRejected, Msg: "none of the relays could be reached; check their addresses (" + strings.Join(relays, ", ") + ")"}
	}
	// The npub is the handle: it identifies the account for good, while a NIP-05 name can move.
	return Credentials{"private_key": hex.EncodeToString(key.Serialize()), "relays": strings.Join(relays, ", ")}, Profile{
		Handle:     npub,
		Name:       fediFirst(meta.DisplayName, meta.Name, npub[:12]+"..."),
		AvatarURL:  meta.Picture,
		ProfileURL: nostrViewer + npub,
	}, nil
}

type nostrMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Picture     string `json:"picture"`
}

// nostrProfile asks the relays for the account's newest kind-0 metadata; reached counts the relays that answered.
func nostrProfile(ctx context.Context, relays []string, pubkey string) (nostrMeta, int) {
	var (
		mu      sync.Mutex
		best    nostrMeta
		newest  int64 = -1
		reached int
		wg      sync.WaitGroup
	)
	for _, relay := range relays {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ev, ok := nostrQuery(ctx, relay, map[string]any{"authors": []string{pubkey}, "kinds": []int{0}, "limit": 1})
			mu.Lock()
			defer mu.Unlock()
			if !ok {
				return
			}
			reached++
			if ev == nil || ev.CreatedAt <= newest {
				return
			}
			var m nostrMeta
			if json.Unmarshal([]byte(ev.Content), &m) == nil {
				best, newest = m, ev.CreatedAt
			}
		}()
	}
	wg.Wait()
	return best, reached
}

type nostrEvent struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// nostrQuery sends one REQ and returns the first event before EOSE; ok is false when the relay could not be reached.
func nostrQuery(ctx context.Context, relay string, filter map[string]any) (*nostrEvent, bool) {
	ctx, cancel := context.WithTimeout(ctx, nostrWait)
	defer cancel()
	c, _, err := websocket.Dial(ctx, relay, &websocket.DialOptions{HTTPClient: httpClient(ctx)})
	if err != nil {
		return nil, false
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	sub := "staqly"
	req, _ := json.Marshal([]any{"REQ", sub, filter})
	if err := c.Write(ctx, websocket.MessageText, req); err != nil {
		return nil, false
	}
	var found *nostrEvent
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return found, true // a relay that connected but went quiet still counts as reached
		}
		var msg []json.RawMessage
		if json.Unmarshal(raw, &msg) != nil || len(msg) < 2 {
			continue
		}
		var typ string
		_ = json.Unmarshal(msg[0], &typ)
		switch typ {
		case "EVENT":
			var ev nostrEvent
			if len(msg) >= 3 && json.Unmarshal(msg[2], &ev) == nil && found == nil {
				found = &ev
			}
		case "EOSE", "CLOSED":
			closeMsg, _ := json.Marshal([]any{"CLOSE", sub})
			_ = c.Write(ctx, websocket.MessageText, closeMsg)
			_ = c.Close(websocket.StatusNormalClosure, "")
			return found, true
		}
	}
}

// Nostr publishes a post's thread as replies under it.
var _ Replier = Nostr{}

func (n Nostr) Publish(ctx context.Context, c Credentials, p PostContent, _ Settings) (Result, error) {
	return n.note(ctx, c, "", p)
}

// Reply answers the post (or the reply before). parentExternalID is the note's id, or "<root id>:<id>" for a reply.
func (n Nostr) Reply(ctx context.Context, c Credentials, parentExternalID string, p PostContent, _ Settings) (Result, error) {
	if parentExternalID == "" {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Nostr: nothing to reply to (the post has no id)"}
	}
	return n.note(ctx, c, parentExternalID, p)
}

var nostrTagRe = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_]*[\p{L}_][\p{L}\p{N}_]*)`)

func (Nostr) note(ctx context.Context, c Credentials, parentID string, p PostContent) (Result, error) {
	key, err := nostrKey(c["private_key"])
	if err != nil {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Nostr: no valid private key stored; reconnect the account"}
	}
	relays, err := nostrRelays(c["relays"])
	if err != nil {
		return Result{}, &Failure{Kind: ErrReauth, Msg: "Nostr: " + err.(*ValidationError).Msg + "; reconnect the account"}
	}
	if len(p.Media) > 0 {
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Nostr channels take text only"}
	}
	pub := hex.EncodeToString(schnorr.SerializePubKey(key.PubKey()))
	tags := [][]string{}
	root := ""
	if parentID != "" {
		r, parent, isReply := strings.Cut(parentID, ":")
		if !isReply {
			parent = r
		}
		if !nostrIsID(r) || !nostrIsID(parent) {
			return Result{}, &Failure{Kind: ErrRejected, Msg: "Nostr: the note replied to has an id staqly can't read (" + parentID + ")"}
		}
		root = r
		// NIP-10 marked tags: the thread's first note is the root, the note answered the reply.
		tags = append(tags, []string{"e", root, "", "root"})
		if parent != root {
			tags = append(tags, []string{"e", parent, "", "reply"})
		}
		tags = append(tags, []string{"p", pub})
	}
	seen := map[string]bool{}
	for _, m := range nostrTagRe.FindAllStringSubmatch(p.Text, -1) {
		if t := strings.ToLower(m[1]); !seen[t] {
			seen[t] = true
			tags = append(tags, []string{"t", t})
		}
	}
	ev := nostrEvent{PubKey: pub, CreatedAt: nostrNow().Unix(), Kind: 1, Tags: tags, Content: p.Text}
	if err := nostrSign(&ev, key); err != nil {
		return Result{}, err
	}

	type answer struct {
		relay                       string
		sent, answered, ok, limited bool
		msg                         string
	}
	answers := make([]answer, len(relays))
	var wg sync.WaitGroup
	for i, relay := range relays {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := answer{relay: relay}
			a.sent, a.answered, a.ok, a.msg = nostrSend(ctx, relay, ev)
			a.limited = strings.HasPrefix(a.msg, "rate-limited:")
			answers[i] = a
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var sent, silent, limited int
	var refusals []string
	for _, a := range answers {
		if a.ok {
			return nostrResult(ev.ID, root), nil
		}
		if !a.sent {
			continue
		}
		sent++
		switch {
		case !a.answered:
			silent++
		case a.limited:
			limited++
			refusals = append(refusals, a.relay+": "+a.msg)
		default:
			refusals = append(refusals, a.relay+": "+a.msg)
		}
	}
	switch {
	case sent == 0:
		return Result{}, &Failure{Msg: "none of the Nostr relays could be reached; staqly tries again in a few minutes"}
	case silent > 0:
		// A relay that took the note but never said OK may have stored it, and a new attempt is a new note (its time,
		// and with it its id, differ): don't send it again.
		return Result{}, &Failure{Kind: ErrRejected, Msg: "Nostr relays may have published the note, but their answer was lost; check your profile before posting it again"}
	case limited == sent:
		return Result{}, &Failure{Msg: "Nostr relays are rate limiting this account; staqly tries again in a few minutes"}
	}
	return Result{}, &Failure{Kind: ErrRejected, Msg: "every Nostr relay refused the note (" + strings.Join(refusals, "; ") + ")"}
}

func nostrIsID(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

func nostrResult(id, root string) Result {
	raw, _ := hex.DecodeString(id)
	note, _ := bech32Encode("note", raw)
	ext := id
	if root != "" {
		ext = root + ":" + id
	}
	return Result{ExternalID: ext, URL: nostrViewer + note}
}

// nostrSend sends the event to one relay and waits for its OK. sent: the event left; answered: the relay said OK
// true or false (a "duplicate:" refusal is a success: the relay has the note already).
func nostrSend(ctx context.Context, relay string, ev nostrEvent) (sent, answered, ok bool, msg string) {
	ctx, cancel := context.WithTimeout(ctx, nostrWait)
	defer cancel()
	c, _, err := websocket.Dial(ctx, relay, &websocket.DialOptions{HTTPClient: httpClient(ctx)})
	if err != nil {
		return false, false, false, "could not connect"
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	raw, _ := json.Marshal([]any{"EVENT", ev})
	if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
		return false, false, false, "could not send"
	}
	for {
		_, in, err := c.Read(ctx)
		if err != nil {
			return true, false, false, "no answer"
		}
		var msg []json.RawMessage
		if json.Unmarshal(in, &msg) != nil || len(msg) < 3 {
			continue
		}
		var typ, id string
		_ = json.Unmarshal(msg[0], &typ)
		_ = json.Unmarshal(msg[1], &id)
		if typ != "OK" || id != ev.ID {
			continue
		}
		var accepted bool
		_ = json.Unmarshal(msg[2], &accepted)
		var text string
		if len(msg) > 3 {
			_ = json.Unmarshal(msg[3], &text)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
		return true, true, accepted || strings.HasPrefix(text, "duplicate:"), text
	}
}

// nostrSign sets the event's id (sha256 of its NIP-01 serialization) and its BIP-340 signature.
func nostrSign(ev *nostrEvent, key *btcec.PrivateKey) error {
	id := sha256.Sum256(nostrSerialize(*ev))
	sig, err := schnorr.Sign(key, id[:])
	if err != nil {
		return fmt.Errorf("signing the note: %w", err)
	}
	ev.ID = hex.EncodeToString(id[:])
	ev.Sig = hex.EncodeToString(sig.Serialize())
	return nil
}

// nostrSerialize is [0,pubkey,created_at,kind,tags,content] as NIP-01 hashes it: no spaces, and strings escaped
// only where JSON must (quotes, backslashes, control characters), so <, >, & and U+2028 stay as they are.
func nostrSerialize(ev nostrEvent) []byte {
	var b strings.Builder
	b.WriteString(`[0,`)
	nostrString(&b, ev.PubKey)
	b.WriteString("," + strconv.FormatInt(ev.CreatedAt, 10) + "," + strconv.Itoa(ev.Kind) + ",[")
	for i, t := range ev.Tags {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		for j, s := range t {
			if j > 0 {
				b.WriteByte(',')
			}
			nostrString(&b, s)
		}
		b.WriteByte(']')
	}
	b.WriteString("],")
	nostrString(&b, ev.Content)
	b.WriteByte(']')
	return []byte(b.String())
}

func nostrString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// bech32 (BIP-173) for nsec, npub and note strings (NIP-19).
const bech32Chars = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []byte) uint32 {
	gen := []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (top>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32Expand(hrp string) []byte {
	out := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

// bech32Regroup turns groups of from bits into groups of to bits (8 to 5 with padding, 5 to 8 without).
func bech32Regroup(data []byte, from, to uint, pad bool) ([]byte, error) {
	var acc, bits uint
	var out []byte
	max := uint(1)<<to - 1
	for _, v := range data {
		acc = acc<<from | uint(v)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&max))
		}
	}
	if pad && bits > 0 {
		out = append(out, byte(acc<<(to-bits)&max))
	} else if !pad && (bits >= from || acc<<(to-bits)&max != 0) {
		return nil, errors.New("bad padding")
	}
	return out, nil
}

func bech32Encode(hrp string, data []byte) (string, error) {
	five, err := bech32Regroup(data, 8, 5, true)
	if err != nil {
		return "", err
	}
	values := append(bech32Expand(hrp), five...)
	mod := bech32Polymod(append(values, 0, 0, 0, 0, 0, 0)) ^ 1
	var b strings.Builder
	b.WriteString(hrp + "1")
	for _, v := range five {
		b.WriteByte(bech32Chars[v])
	}
	for i := 0; i < 6; i++ {
		b.WriteByte(bech32Chars[(mod>>(5*(5-i)))&31])
	}
	return b.String(), nil
}

func bech32Decode(s string) (string, []byte, error) {
	s = strings.ToLower(s)
	pos := strings.LastIndexByte(s, '1')
	if pos < 1 || pos+7 > len(s) {
		return "", nil, errors.New("not bech32")
	}
	hrp := s[:pos]
	var data []byte
	for _, c := range s[pos+1:] {
		i := strings.IndexRune(bech32Chars, c)
		if i < 0 {
			return "", nil, errors.New("not bech32")
		}
		data = append(data, byte(i))
	}
	if bech32Polymod(append(bech32Expand(hrp), data...)) != 1 {
		return "", nil, errors.New("bad checksum")
	}
	raw, err := bech32Regroup(data[:len(data)-6], 5, 8, false)
	return hrp, raw, err
}
