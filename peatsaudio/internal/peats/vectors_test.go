package peats

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The golden vectors live at the repository root, outside this module, because
// the Kotlin implementation reads the very same file. A case added there is
// checked against both implementations at once — that is what keeps the two
// from drifting apart.
var vectorsPath = filepath.Join("..", "..", "..", "testvectors", "protocol.json")

type vecFrame struct {
	Version string `json:"version"`
	Flags   string `json:"flags"`
	Vendor  string `json:"vendor"`
	Command string `json:"command"`
	Payload string `json:"payload"`
}

type vecPair struct {
	Code   string `json:"code"`
	Active bool   `json:"active"`
}

type vecMapping struct {
	Gesture string `json:"gesture"`
	Action  string `json:"action"`
}

type vectors struct {
	Encode []struct {
		Name    string `json:"name"`
		Command string `json:"command"`
		Vendor  string `json:"vendor"`
		Payload string `json:"payload"`
		Frame   string `json:"frame"`
	} `json:"encode"`
	Response []struct {
		Command  string `json:"command"`
		Response string `json:"response"`
	} `json:"response"`
	Decode []struct {
		Name   string     `json:"name"`
		Stream string     `json:"stream"`
		Frames []vecFrame `json:"frames"`
		Rest   string     `json:"rest"`
	} `json:"decode"`
	Scalar []struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
		Value   int    `json:"value"`
	} `json:"scalar"`
	Pairs []struct {
		Name    string    `json:"name"`
		Payload string    `json:"payload"`
		Pairs   []vecPair `json:"pairs"`
		Active  *string   `json:"active"`
	} `json:"pairs"`
	KeyMappings []struct {
		Name     string       `json:"name"`
		Payload  string       `json:"payload"`
		Mappings []vecMapping `json:"mappings"`
	} `json:"keyMappings"`
	EncodeKeyMappings []struct {
		Name     string       `json:"name"`
		Mappings []vecMapping `json:"mappings"`
		Payload  string       `json:"payload"`
	} `json:"encodeKeyMappings"`
	Firmware []struct {
		Name     string `json:"name"`
		Payload  string `json:"payload"`
		Firmware string `json:"firmware"`
	} `json:"firmware"`
	EQBand []struct {
		Name       string  `json:"name"`
		Total      int     `json:"total"`
		Index      int     `json:"index"`
		MasterGain float64 `json:"masterGain"`
		Band       EQBand  `json:"band"`
		V1         bool    `json:"v1"`
		Payload    string  `json:"payload"`
	} `json:"eqBand"`
	EQUsesV1 []struct {
		Firmware string `json:"firmware"`
		V1       bool   `json:"v1"`
	} `json:"eqUsesV1"`
}

func loadVectors(t *testing.T) vectors {
	t.Helper()
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatalf("reading the shared vectors: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parsing %s: %v", vectorsPath, err)
	}
	return v
}

// unhex decodes a vector's hex string, spaces allowed.
func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q in the vectors: %v", s, err)
	}
	return b
}

func unhexByte(t *testing.T, s string) byte {
	t.Helper()
	b := unhex(t, s)
	if len(b) != 1 {
		t.Fatalf("%q is not a single byte", s)
	}
	return b[0]
}

func unhexCmd(t *testing.T, s string) Command {
	t.Helper()
	b := unhex(t, s)
	if len(b) != 2 {
		t.Fatalf("%q is not a two-byte opcode", s)
	}
	return Command{b[0], b[1]}
}

func TestVectorsEncode(t *testing.T) {
	for _, c := range loadVectors(t).Encode {
		vendor := unhexCmd(t, c.Vendor)
		got := Encode(unhexCmd(t, c.Command), unhex(t, c.Payload), [2]byte(vendor))
		if want := unhex(t, c.Frame); !bytes.Equal(got, want) {
			t.Errorf("%s: Encode = % X, want % X", c.Name, got, want)
		}
	}
}

func TestVectorsResponse(t *testing.T) {
	for _, c := range loadVectors(t).Response {
		if got, want := unhexCmd(t, c.Command).Response(), unhexCmd(t, c.Response); got != want {
			t.Errorf("Response(%s) = %s, want %s", c.Command, got, want)
		}
	}
}

// TestVectorsDecode feeds each stream through the same accumulate-and-decode
// loop readLoop runs, so the vectors pin down what the client actually keeps.
func TestVectorsDecode(t *testing.T) {
	for _, c := range loadVectors(t).Decode {
		acc := unhex(t, c.Stream)
		var got []Frame
		for {
			f, used, err := Decode(acc)
			if err != nil {
				acc = acc[used:]
				break
			}
			acc = acc[used:]
			got = append(got, f)
		}

		if len(got) != len(c.Frames) {
			t.Errorf("%s: %d frames, want %d", c.Name, len(got), len(c.Frames))
			continue
		}
		for i, w := range c.Frames {
			g := got[i]
			if g.Version != unhexByte(t, w.Version) || g.Flags != unhexByte(t, w.Flags) ||
				g.Vendor != [2]byte(unhexCmd(t, w.Vendor)) || g.Command != unhexCmd(t, w.Command) ||
				!bytes.Equal(g.Payload, unhex(t, w.Payload)) {
				t.Errorf("%s: frame %d = %+v, want %+v", c.Name, i, g, w)
			}
		}
		if want := unhex(t, c.Rest); !bytes.Equal(acc, want) {
			t.Errorf("%s: left % X in the buffer, want % X", c.Name, acc, want)
		}
	}
}

func TestVectorsScalar(t *testing.T) {
	for _, c := range loadVectors(t).Scalar {
		if got := (Frame{Payload: unhex(t, c.Payload)}).Scalar(); int(got) != c.Value {
			t.Errorf("%s: Scalar = %d, want %d", c.Name, got, c.Value)
		}
	}
}

func TestVectorsPairs(t *testing.T) {
	for _, c := range loadVectors(t).Pairs {
		f := Frame{Payload: unhex(t, c.Payload)}
		got := f.Pairs()
		if len(got) != len(c.Pairs) {
			t.Errorf("%s: %d pairs, want %d", c.Name, len(got), len(c.Pairs))
			continue
		}
		for i, w := range c.Pairs {
			if got[i] != (Pair{Code: unhexByte(t, w.Code), Active: w.Active}) {
				t.Errorf("%s: pair %d = %+v, want %+v", c.Name, i, got[i], w)
			}
		}

		code, ok := f.ActiveCode()
		switch {
		case c.Active == nil && ok:
			t.Errorf("%s: active = %02X, want none", c.Name, code)
		case c.Active != nil && (!ok || code != unhexByte(t, *c.Active)):
			t.Errorf("%s: active = %02X (found %v), want %s", c.Name, code, ok, *c.Active)
		}
	}
}

func toMappings(t *testing.T, vs []vecMapping) []KeyMapping {
	out := make([]KeyMapping, 0, len(vs))
	for _, m := range vs {
		out = append(out, KeyMapping{Gesture: unhexByte(t, m.Gesture), Action: unhexByte(t, m.Action)})
	}
	return out
}

func TestVectorsKeyMappings(t *testing.T) {
	v := loadVectors(t)
	for _, c := range v.KeyMappings {
		got := (Frame{Payload: unhex(t, c.Payload)}).KeyMappings()
		want := toMappings(t, c.Mappings)
		if len(got) != len(want) {
			t.Errorf("%s: %d mappings, want %d", c.Name, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: mapping %d = %+v, want %+v", c.Name, i, got[i], want[i])
			}
		}
	}
	for _, c := range v.EncodeKeyMappings {
		got := EncodeKeyMappings(toMappings(t, c.Mappings))
		if want := unhex(t, c.Payload); !bytes.Equal(got, want) {
			t.Errorf("%s: EncodeKeyMappings = % X, want % X", c.Name, got, want)
		}
	}
}

func TestVectorsFirmware(t *testing.T) {
	for _, c := range loadVectors(t).Firmware {
		if got := (Frame{Payload: unhex(t, c.Payload)}).Firmware(); got != c.Firmware {
			t.Errorf("%s: Firmware = %q, want %q", c.Name, got, c.Firmware)
		}
	}
}

func TestVectorsEQBand(t *testing.T) {
	for _, c := range loadVectors(t).EQBand {
		got := EncodeEQBand(c.Total, c.Index, c.MasterGain, c.Band, c.V1)
		if want := unhex(t, c.Payload); !bytes.Equal(got, want) {
			t.Errorf("%s: EncodeEQBand = % X, want % X", c.Name, got, want)
		}
	}
}

func TestVectorsEQUsesV1(t *testing.T) {
	for _, c := range loadVectors(t).EQUsesV1 {
		if got := EQUsesV1(c.Firmware); got != c.V1 {
			t.Errorf("EQUsesV1(%q) = %v, want %v", c.Firmware, got, c.V1)
		}
	}
}
