package session

import "testing"

func TestNormalizeColorRejectsJunk(t *testing.T) {
	ok := map[string]string{"red": "red", "gray": "gray", "none": "none", "#A1b2C3": "#a1b2c3", "#000000": "#000000"}
	for in, want := range ok {
		if got := NormalizeColor(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "Red", "#fff", "#12345g", "#1234567", "rgb(1,2,3)", "url(x)", "red;background:url(x)",
		"var(--x)", " #123456", "#123456 ", "ﬀ0000", "javascript:alert(1)"} {
		if got := NormalizeColor(bad); got != "" {
			t.Errorf("%q passed as %q", bad, got)
		}
	}
}

func TestNormalizeIconRejectsJunk(t *testing.T) {
	for k := range ConnectionIcons {
		if NormalizeIcon(k) != k {
			t.Errorf("%s rejected", k)
		}
	}
	for _, bad := range []string{"", "OLT", "olt.svg", "../olt", "/tmp/x.png", "https://x/y.svg", "<svg onload=x>", "data:image/svg+xml,x", "router "} {
		if got := NormalizeIcon(bad); got != "" {
			t.Errorf("%q passed as %q", bad, got)
		}
	}
	if NormalizeIcon("none") != "none" {
		t.Error("none")
	}
}

func TestSanitizeStyles(t *testing.T) {
	d := ConnectionStoreData{
		Groups:      []ConnectionGroup{{ID: "g", Color: "#ZZZZZZ", Icon: "olt"}},
		Connections: []ConnectionConfig{{ID: "c", Color: "#ABCDEF", Icon: "evil.svg"}},
	}
	SanitizeStyles(&d)
	if d.Groups[0].Color != "" || d.Groups[0].Icon != "olt" || d.Connections[0].Color != "#abcdef" || d.Connections[0].Icon != "" {
		t.Fatalf("%+v %+v", d.Groups[0], d.Connections[0])
	}
}

func TestEffectiveStyleInheritance(t *testing.T) {
	p := func(s string) *string { return &s }
	groups := []ConnectionGroup{
		{ID: "pop", Color: "orange", Icon: "router"},
		{ID: "olts", ParentId: p("pop"), Icon: "olt"},
		{ID: "lab", ParentId: p("pop"), Color: "none", Icon: "none"},
		{ID: "loop1", ParentId: p("loop2")},
		{ID: "loop2", ParentId: p("loop1")},
	}
	cases := []struct {
		c           ConnectionConfig
		color, icon string
	}{
		{ConnectionConfig{GroupId: p("olts")}, "orange", "olt"},            // icon from the group, color from its parent
		{ConnectionConfig{GroupId: p("olts"), Color: "red"}, "red", "olt"}, // own color wins
		{ConnectionConfig{GroupId: p("lab")}, "", ""},                      // "none" stops inheritance
		{ConnectionConfig{GroupId: p("pop"), Icon: "none"}, "orange", ""},  // own "none"
		{ConnectionConfig{GroupId: p("loop1")}, "", ""},                    // cycle ends
		{ConnectionConfig{GroupId: p("gone")}, "", ""},
		{ConnectionConfig{}, "", ""},
		{ConnectionConfig{GroupId: p("pop"), Color: "junk"}, "orange", "router"}, // junk counts as unset
	}
	for i, c := range cases {
		color, icon := EffectiveStyle(c.c, groups)
		if color != c.color || icon != c.icon {
			t.Errorf("%d: got %q %q, want %q %q", i, color, icon, c.color, c.icon)
		}
	}
}
