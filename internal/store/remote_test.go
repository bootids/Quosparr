package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestRemoteSourceMissingKeyDecodesToNil(t *testing.T) {
	for _, doc := range []string{`{}`, `{"language":"english"}`} {
		if p := mustDecode(t, doc); p.RemoteSource != nil {
			t.Errorf("%s decoded remoteSource to %+v, want nil", doc, *p.RemoteSource)
		}
	}
	if DefaultPreferences().RemoteSource != nil {
		t.Error("default preferences preset a remote source")
	}
}

func TestRemoteSourceDecodesLeniently(t *testing.T) {
	for _, raw := range []string{`null`, `12`, `"x"`, `[]`, `{"enabled":"yes"}`, `{"host":1}`} {
		if p := mustDecode(t, `{"remoteSource":`+raw+`}`); p.RemoteSource != nil {
			t.Errorf("remoteSource %s decoded to %+v, want nil", raw, *p.RemoteSource)
		}
	}
}

func TestRemoteSourceZeroObjectIsNotWritten(t *testing.T) {
	p := DefaultPreferences()
	p.RemoteSource = &RemoteSource{}
	enc, err := EncodePreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(enc), "remoteSource") {
		t.Errorf("the zero remote source is written: %s", enc)
	}
}

func TestRemoteSourceRoundTrips(t *testing.T) {
	p := DefaultPreferences()
	p.RemoteSource = &RemoteSource{Enabled: true, Host: "user@host.example", Port: 2222, Dir: "/srv/quosparr"}
	enc, err := EncodePreferences(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustDecode(t, string(enc)).RemoteSource; !reflect.DeepEqual(got, p.RemoteSource) {
		t.Errorf("round trip = %+v, want %+v", got, *p.RemoteSource)
	}
	// Port 0 and an empty dir are values, not defaults: the object stays.
	p.RemoteSource = &RemoteSource{Enabled: false, Host: "host.example"}
	if enc, err = EncodePreferences(p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enc), `"remoteSource":{"enabled":false,"host":"host.example","port":0,"dir":""}`) {
		t.Errorf("unset remote source wrote %s", enc)
	}
}

func TestRemoteSourceCloneIsDeep(t *testing.T) {
	p := DefaultPreferences()
	p.RemoteSource = &RemoteSource{Enabled: true, Host: "host.example", Port: 22, Dir: "/data"}
	c := p.Clone()
	c.RemoteSource.Host = "other.example"
	c.RemoteSource.Enabled = false
	if p.RemoteSource.Host != "host.example" || !p.RemoteSource.Enabled {
		t.Errorf("clone shares the remote source: %+v", *p.RemoteSource)
	}
}

func TestRemoteSourceValidate(t *testing.T) {
	for _, tc := range []struct {
		src  RemoteSource
		want bool
	}{
		{RemoteSource{}, true},
		{RemoteSource{Host: "host.example"}, true},
		{RemoteSource{Enabled: true, Host: "user@host"}, true},
		{RemoteSource{Enabled: true, Host: "host", Port: 1}, true},
		{RemoteSource{Enabled: true, Host: "host", Port: 65535}, true},
		{RemoteSource{Enabled: true}, false},
		{RemoteSource{Enabled: true, Host: "host", Port: -1}, false},
		{RemoteSource{Enabled: true, Host: "host", Port: 65536}, false},
		{RemoteSource{Enabled: true, Host: "host\x00"}, false},
		{RemoteSource{Enabled: true, Host: "host", Dir: "a\x1fb"}, false},
		{RemoteSource{Port: 22}, true},
	} {
		if got := tc.src.validate() == nil; got != tc.want {
			t.Errorf("validate(%+v) ok = %v, want %v", tc.src, got, tc.want)
		}
	}
	p := DefaultPreferences()
	p.RemoteSource = &RemoteSource{Enabled: true}
	if _, err := EncodePreferences(p); err == nil {
		t.Error("encoding a remote source without a host succeeded")
	}
}
