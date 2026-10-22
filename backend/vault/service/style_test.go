package service

import (
	"encoding/json"
	"testing"

	"github.com/ys-ll/uniterm/backend/vault"
	"github.com/ys-ll/uniterm/backend/vault/records"
)

// olderBridge stands in for a build whose stores don't know some fields:
// they are dropped on write, as encoding/json does with unknown keys.
type olderBridge struct {
	memBridge
	unknown string
}

func (b *olderBridge) Apply(s records.Snapshot) error {
	out := records.Snapshot{}
	for k, f := range s {
		g := records.Fields{}
		for name, v := range f {
			if name != b.unknown {
				g[name] = v
			}
		}
		out[k] = g
	}
	return b.memBridge.Apply(out)
}

func (b *olderBridge) Keeps(_ records.Kind, field string) bool { return field != b.unknown }

func colorOf(t *testing.T, b *memBridge, id string) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	raw, ok := b.snap[records.Key{Kind: records.Connection, ID: id}]["color"]
	if !ok {
		return ""
	}
	var c string
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func setColor(b *memBridge, id, color string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f := b.snap[records.Key{Kind: records.Connection, ID: id}]
	v, _ := json.Marshal(color)
	f["color"] = v
}

// A build that doesn't know a field must not clear it for everyone.
func TestUnknownFieldIsNotSentBackAsNull(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	old := &olderBridge{unknown: "color"}
	b.bridge = &old.memBridge
	b.o.Bridge = old

	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1"})
	create(t, a)
	_, err := a.Sync()
	must(t, err)
	approve(t, a, b, vault.RoleWriter)
	_, err = b.Sync()
	must(t, err)

	setColor(a.bridge, "c1", "orange")
	_, err = a.Sync()
	must(t, err)
	// B gets the op, drops color in its stores, then edits another field.
	_, err = b.Sync()
	must(t, err)
	if colorOf(t, b.bridge, "c1") != "" {
		t.Fatal("the older bridge kept the unknown field")
	}
	b.bridge.put(t, conn{ID: "c1", Name: "olt-pjt", Host: "10.0.0.1"})
	_, err = b.Sync()
	must(t, err)
	_, err = b.Sync()
	must(t, err)
	_, err = a.Sync()
	must(t, err)
	if c, _ := a.bridge.get(t, "c1"); c.Name != "olt-pjt" {
		t.Fatalf("a c1 = %+v", c)
	}
	if got := colorOf(t, a.bridge, "c1"); got != "orange" {
		t.Fatalf("color after the older device synced = %q", got)
	}
}

// Two devices that know the field: last write wins, per field.
func TestColorLastWriteWins(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1"})
	create(t, a)
	_, err := a.Sync()
	must(t, err)
	approve(t, a, b, vault.RoleWriter)
	_, err = b.Sync()
	must(t, err)

	setColor(a.bridge, "c1", "red")
	_, err = a.Sync()
	must(t, err)
	_, err = b.Sync()
	must(t, err)
	if colorOf(t, b.bridge, "c1") != "red" {
		t.Fatal("b did not get red")
	}
	// B changes color, A changes the host: both survive.
	setColor(b.bridge, "c1", "#30a46c")
	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.2"})
	setColor(a.bridge, "c1", "red")
	_, err = b.Sync()
	must(t, err)
	_, err = a.Sync()
	must(t, err)
	_, err = b.Sync()
	must(t, err)
	for _, d := range []*device{a, b} {
		c, _ := d.bridge.get(t, "c1")
		if colorOf(t, d.bridge, "c1") != "#30a46c" || c.Host != "10.0.0.2" {
			t.Fatalf("%s: color %q host %q", d.Status().Device.Name, colorOf(t, d.bridge, "c1"), c.Host)
		}
	}
}

// sanitizingBridge stands in for stores that accept a field but not every
// value: a newer build's palette color becomes "" here, as SanitizeStyles
// does.
type sanitizingBridge struct {
	memBridge
	applies int
}

func (b *sanitizingBridge) Apply(s records.Snapshot) error {
	b.applies++
	out := records.Snapshot{}
	for k, f := range s {
		g := records.Fields{}
		for name, v := range f {
			if name == "color" && string(v) == `"pink"` {
				continue // "" is omitted on save
			}
			g[name] = v
		}
		out[k] = g
	}
	return b.memBridge.Apply(out)
}

func (b *sanitizingBridge) Keeps(records.Kind, string) bool { return true }

// A value this build doesn't accept is neither cleared for everyone nor
// written again every round.
func TestUnknownValueIsNotClearedNorReapplied(t *testing.T) {
	shared := t.TempDir()
	a := newDevice(t, shared, "mac-a")
	b := newDevice(t, shared, "mac-b")
	sb := &sanitizingBridge{}
	b.bridge = &sb.memBridge
	b.o.Bridge = sb

	a.bridge.put(t, conn{ID: "c1", Name: "olt", Host: "10.0.0.1"})
	create(t, a)
	_, err := a.Sync()
	must(t, err)
	approve(t, a, b, vault.RoleWriter)
	_, err = b.Sync()
	must(t, err)

	setColor(a.bridge, "c1", "pink") // a newer build's palette
	_, err = a.Sync()
	must(t, err)
	_, err = b.Sync()
	must(t, err)
	if colorOf(t, b.bridge, "c1") != "" {
		t.Fatal("the stores kept a value they don't accept")
	}
	n := sb.applies
	for range 3 {
		res, err := b.Sync()
		must(t, err)
		if res.Pulled != 0 {
			t.Fatalf("pulled %d with nothing new", res.Pulled)
		}
	}
	if sb.applies != n {
		t.Fatalf("applied %d more times with nothing new", sb.applies-n)
	}
	// B edits something else; A keeps pink.
	b.bridge.put(t, conn{ID: "c1", Name: "olt-pjt", Host: "10.0.0.1"})
	_, err = b.Sync()
	must(t, err)
	_, err = a.Sync()
	must(t, err)
	if c, _ := a.bridge.get(t, "c1"); c.Name != "olt-pjt" || colorOf(t, a.bridge, "c1") != "pink" {
		t.Fatalf("a: %+v color %q", c, colorOf(t, a.bridge, "c1"))
	}
}
