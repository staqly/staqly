package app

import (
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The check of an editor file answers as decoding it whole did.
func TestReadDesignFileMatchesDecoding(t *testing.T) {
	for _, file := range []string{
		`{"version":1,"width":1080,"height":1350,"pages":[{"nodes":[{"text":"a \"quoted\" é \\ ] }"}]}]}`,
		` { "pages" : [ 1 , [ ] , { } , "x" , null , true , -1.5e3 ] , "height" : 2 , "width" : 1e2 , "version" : 1 } trailing`,
		`{"version":1,"kind":"video","width":1,"height":1,"clips":[{}],"sounds":[]}`,
		`{"version":1,"kind":"video","width":1,"height":1,"clips":[],"sounds":[]}`,
		`{"version":1,"kind":"audio","sounds":[{"a":[1,2,{"b":null}]}]}`,
		`{"version":1,"kind":"audio","sounds":null}`,
		`{"version":1,"kind":"photo","width":1,"height":1,"pages":[1]}`,
		`{"version":2,"width":1,"height":1,"pages":[1]}`,
		`{"version":1,"kind":5,"width":1,"height":1,"pages":[1]}`,
		`{"version":1,"kind":null,"width":1,"height":1,"pages":[1]}`,
		`{"version":1,"kind":["video"],"width":1,"height":1,"pages":[1]}`,
		`{"VERSION":1,"Width":1,"height":1,"pages":{}}`,
		`{"version":1,"width":0,"height":1,"pages":[1]}`,
		`{"version":1,"width":1,"height":1,"pages":[]}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"pages":[]}`,
		`{"version":1,"width":"1","height":1,"pages":[1]}`,
		`{"version":1,"width":1,"height":1,"pages":[1,]}`,
		`{"version":1,"width":1,"height":1,"pages":[1] "x":1}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"x":"a` + "\n" + `b"}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"x":"\q"}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"x":"\u12G4"}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"x":01}`,
		`{"version":1,"width":1,"height":1,"pages":[1],"x":tru}`,
		`{"version":1,"width":1,"height":1,"pages":[1]`,
		`{"version":1,"width":1,"height":1,"pages":[{"a":}]}`,
		`[1]`, `"x"`, ``, `not a project`, "GPEX\x01\x00\x14\x00x",
	} {
		var whole struct {
			Version              int
			Kind                 string
			Width, Height        float64
			Pages, Clips, Sounds []json.RawMessage
		}
		want := json.NewDecoder(strings.NewReader(file)).Decode(&whole) == nil && designFile{Version: float64(whole.Version), Kind: whole.Kind,
			Width: whole.Width, Height: whole.Height, Pages: len(whole.Pages), Clips: len(whole.Clips), Sounds: len(whole.Sounds)}.valid()
		d, err := readDesignFile(strings.NewReader(file))
		if got := err == nil && d.valid(); got != want {
			t.Errorf("%q: an editor project %v, decoding it says %v (%+v, %v)", file, got, want, d, err)
		}
	}
}

type letters struct{}

func (letters) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// Checking a large project must not hold it in memory: decoding one 100 MB file held 447 MB of heap, so a few
// uploads at once could take the server down.
func TestReadDesignFileHoldsNoFile(t *testing.T) {
	const size = 64 << 20
	file := io.MultiReader(strings.NewReader(`{"version":1,"width":1,"height":1,"pages":["`), io.LimitReader(letters{}, size), strings.NewReader(`"]}`))
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				peak.Store(max(peak.Load(), m.HeapAlloc))
			}
		}
	}()
	d, err := readDesignFile(file)
	close(stop)
	<-stopped
	if err != nil || !d.valid() {
		t.Fatalf("a valid project: %+v %v", d, err)
	}
	if grew := int64(peak.Load()-base.HeapAlloc) >> 20; grew > 8 {
		t.Fatalf("checking a %d MB project held %d MB of heap", size>>20, grew)
	}
}
