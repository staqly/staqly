package app

import "testing"

// The library takes pictures, videos and audio; sniff must tell audio containers from video ones.
func TestSniffAudio(t *testing.T) {
	for _, c := range []struct {
		head []byte
		name string
		want string
	}{
		{[]byte("OggS\x00\x02\x00\x00\x00\x00\x00\x00\x00\x00"), "voice.ogg", "audio/ogg"},
		{[]byte("ID3\x03\x00\x00\x00\x00\x00\x00"), "song.mp3", "audio/mpeg"},
		{[]byte{0xFF, 0xFB, 0x90, 0x64, 0, 0, 0, 0}, "song.mp3", "audio/mpeg"},
		{[]byte{0xFF, 0xFB, 0x90, 0x64, 0, 0, 0, 0}, "song.bin", "application/octet-stream"},
		{[]byte("RIFF\x24\x00\x00\x00WAVEfmt "), "take.wav", "audio/wave"},
		{[]byte("\x00\x00\x00\x20ftypM4A \x00\x00\x00\x00"), "memo.m4a", "audio/mp4"},
		{[]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00"), "clip.mp4", "video/mp4"},
		{[]byte("hello world, plain text"), "notes.txt", "text/plain"},
	} {
		got := sniff(c.head, c.name)
		if got != c.want {
			t.Errorf("sniff(%s) = %s, want %s", c.name, got, c.want)
		}
		_, allowed := allowedMedia[got]
		if wantAllowed := got != "text/plain" && got != "application/octet-stream"; allowed != wantAllowed {
			t.Errorf("%s allowed = %v", c.name, allowed)
		}
	}
}
