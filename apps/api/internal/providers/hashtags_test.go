package providers

import "testing"

func TestAppendHashtags(t *testing.T) {
	for _, tc := range []struct {
		text, want string
		tags       []string
	}{
		{"Launch day\n", "Launch day\n\n#launch #clienta #Ünïcode", []string{"launch", "client-a", "#Ünïcode"}},
		{"Out now #Launch", "Out now #Launch\n\n#dev", []string{"launch", "dev", "Dev"}}, // already there, any case; once
		{"Out now", "Out now", []string{"--", ""}},
		{"", "#solo", []string{"solo"}},
		{"See #launches", "See #launches\n\n#launch", []string{"launch"}}, // a longer hashtag is not this one
	} {
		if got := AppendHashtags(tc.text, tc.tags); got != tc.want {
			t.Errorf("AppendHashtags(%q, %q) = %q, want %q", tc.text, tc.tags, got, tc.want)
		}
	}
}
