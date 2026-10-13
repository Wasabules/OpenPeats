package peats

import (
	"errors"
	"slices"
	"testing"
)

func TestIsAudioVideo(t *testing.T) {
	cases := []struct {
		name  string
		class uint32
		want  bool
	}{
		{"wearable headset", 0x240404, true},
		{"headphones", 0x240418, true},
		{"loudspeaker", 0x240414, true},
		{"smartphone", 0x5a020c, false},
		{"keyboard", 0x002540, false},
		{"unknown class", 0, false},
	}
	for _, c := range cases {
		if got := isAudioVideo(c.class); got != c.want {
			t.Errorf("%s (%06X): isAudioVideo = %v, want %v", c.name, c.class, got, c.want)
		}
	}
}

func TestFormatMACRoundTrip(t *testing.T) {
	const s = "98:80:BB:40:D4:ED"
	addr, err := ParseMAC("98:80:bb:40:d4:ed")
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatMAC(addr); got != s {
		t.Errorf("FormatMAC = %q, want %q", got, s)
	}
}

// Only audio devices are listed, connected first, then by name.
func TestPairedAudioDevicesFiltersAndSorts(t *testing.T) {
	prev := listPaired
	defer func() { listPaired = prev }()
	listPaired = func() ([]pairedDevice, error) {
		return []pairedDevice{
			{Device{Name: "Phone", Address: "00:00:00:00:00:01", Connected: true}, 0x5a020c},
			{Device{Name: "speaker", Address: "00:00:00:00:00:02"}, 0x240414},
			{Device{Name: "Earbuds", Address: "00:00:00:00:00:03"}, 0x240404},
			{Device{Name: "Headset", Address: "00:00:00:00:00:04", Connected: true}, 0x240404},
		}, nil
	}

	got, err := PairedAudioDevices()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Name)
	}
	if want := []string{"Headset", "Earbuds", "speaker"}; !slices.Equal(names, want) {
		t.Errorf("listed %v, want %v", names, want)
	}
}

// An empty list is a list, not a null the frontend would have to special-case.
func TestPairedAudioDevicesEmptyIsNotNil(t *testing.T) {
	prev := listPaired
	defer func() { listPaired = prev }()
	listPaired = func() ([]pairedDevice, error) { return nil, nil }

	got, err := PairedAudioDevices()
	if err != nil || got == nil {
		t.Errorf("PairedAudioDevices = %v, %v; want an empty, non-nil list", got, err)
	}
}

func TestPairedAudioDevicesUnsupported(t *testing.T) {
	prev := listPaired
	defer func() { listPaired = prev }()
	listPaired = nil

	if _, err := PairedAudioDevices(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}
