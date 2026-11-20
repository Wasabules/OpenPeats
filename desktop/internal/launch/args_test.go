package launch

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		args   []string
		want   Options
		window bool
	}{
		{nil, Options{ANCMode: -1}, true},
		{[]string{"--background"}, Options{Background: true, ANCMode: -1}, false},
		{[]string{"--anc=2"}, Options{ANCMode: 2}, false},
		{[]string{"--anc", "0"}, Options{ANCMode: 0}, false},
		// What `wails dev` adds is not ours to refuse.
		{[]string{"-assetdir", "x", "--background"}, Options{Background: true, ANCMode: -1}, false},
		{[]string{"--install-integration"}, Options{ANCMode: -1, Integration: "install"}, false},
		{[]string{"--remove-integration"}, Options{ANCMode: -1, Integration: "remove"}, false},
	}
	for _, tt := range tests {
		got, err := Parse(tt.args)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.args, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
		}
		if got.Window() != tt.window {
			t.Errorf("Parse(%q).Window() = %v", tt.args, got.Window())
		}
	}
}

func TestParseRejectsModesTheEarbudsDoNotHave(t *testing.T) {
	for _, args := range [][]string{
		{"--anc=3"}, {"--anc=-1"}, {"--anc=on"}, {"--anc"},
	} {
		if _, err := Parse(args); err != ErrInvalidANC {
			t.Errorf("Parse(%q): got %v, want ErrInvalidANC", args, err)
		}
	}
}
