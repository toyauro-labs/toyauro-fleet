package fleet

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestQuoteSurvivesAShell(t *testing.T) {
	for _, in := range []string{"plain", "#{pane_id}", "a b", "it's", ";", "%1", "", "`id`", "$(id)", "你好"} {
		out, err := exec.Command("sh", "-c", "printf %s "+quote(in)).Output()
		if err != nil || string(out) != in {
			t.Errorf("quote(%q) round-tripped to %q, err %v", in, out, err)
		}
	}
}

func TestParseAddress(t *testing.T) {
	host, server, pane, err := ParseAddress("macmini/vibe/%127")
	if err != nil || host != "macmini" || server != "vibe" || pane != "%127" {
		t.Fatalf("got %q %q %q %v", host, server, pane, err)
	}
	for _, bad := range []string{"", "a/b", "/s/%1", "a//%1", "a/s/1", "a/b c/%1", "a/s/%1/x", "a/$(id)/%1"} {
		if _, _, _, err := ParseAddress(bad); err == nil {
			t.Errorf("ParseAddress(%q) accepted", bad)
		}
	}
}

func TestParseMessagesSkipsWhatIsNotAMessage(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	line := func(id, created string) string {
		return Prefix + id + "\t" + `{"version":1,"id":"` + id + `","to_pane":"%1","created":"` + created + `","body":"x"}`
	}
	out := strings.Join([]string{
		line(b, "2026-11-01T10:00:02"),
		line(a, "2026-11-01T10:00:01"),
		Prefix + strings.Repeat("c", 32) + "\t",                // acknowledged while being read
		Prefix + strings.Repeat("d", 32) + "\tnot json",        // foreign buffer
		Prefix + strings.Repeat("e", 32) + "\t" + `{"id":"f"}`, // wrong ID
		"stray line from a multi-line buffer",
		line(strings.Repeat("9", 32), "x")[:40],
	}, "\n")
	got := parseMessages(out)
	if len(got) != 2 || got[0].ID != a || got[1].ID != b {
		t.Fatalf("got %+v", got)
	}
}

func TestReplyTargetResolvesDotAgainstTheMailbox(t *testing.T) {
	if got := replyTarget(Target{Host: "phone", Server: "x"}, ".", "y"); got != (Target{Host: "phone", Server: "y"}) {
		t.Errorf("dot under --host: %+v", got)
	}
	if got := replyTarget(Target{Server: "x"}, ".", "y"); got != (Target{Server: "y"}) {
		t.Errorf("dot locally: %+v", got)
	}
	if got := replyTarget(Target{Host: "phone"}, "mac", "y"); got != (Target{Host: "mac", Server: "y"}) {
		t.Errorf("alias: %+v", got)
	}
}

func TestParseNodes(t *testing.T) {
	nodes, err := parseNodes([]byte(`{"nodes":[{"name":"mac","server":"vibe"},{"name":"phone","host":"ace-ts","return_host":"macmini"},{"name":"h","host":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Node{{Name: "mac", Server: "vibe"}, {Name: "phone", Host: "ace-ts", Server: "default", ReturnHost: "macmini"}, {Name: "h", Server: "default"}}
	for i := range want {
		if nodes[i] != want[i] {
			t.Errorf("node %d: %+v", i, nodes[i])
		}
	}
	for _, bad := range []string{`{`, `{}`, `{"nodes":[]}`, `{"nodes":[{"host":"a"}]}`, `{"nodes":[{"name":"a"},{"name":"a"}]}`,
		`{"nodes":[{"name":"a","host":"-oProxyCommand=x"}]}`, `{"nodes":[{"name":"a","host":""}]}`, `{"nodes":[{"name":"a","server":"b c"}]}`} {
		if _, err := parseNodes([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestParseDest(t *testing.T) {
	for in, want := range map[string]struct {
		host string
		port int
	}{
		"mac":                 {"mac", 0},
		"jeff@100.88.205.23":  {"jeff@100.88.205.23", 0},
		"100.111.45.97:8022":  {"100.111.45.97", 8022},
		"u@host.example:2222": {"u@host.example", 2222},
	} {
		host, port, err := ParseDest(in)
		if err != nil || host != want.host || port != want.port {
			t.Errorf("ParseDest(%q) = %q %d %v", in, host, port, err)
		}
	}
	for _, bad := range []string{"", "-oProxyCommand=x", "a b", "h:0", "h:70000", "h:x", "a/b", "$(id)", "u@-x"} {
		if _, _, err := ParseDest(bad); err == nil {
			t.Errorf("ParseDest(%q) accepted", bad)
		}
	}
}

func TestSaveNodesRoundTrips(t *testing.T) {
	t.Setenv("FLEET_CONFIG", filepath.Join(t.TempDir(), "sub", "config.json"))
	want := []Node{
		{Name: "here", Server: "vibe"},
		{Name: "phone", Host: "u@1.2.3.4", Port: 8022, Server: "default", ReturnHost: "mac"},
	}
	if err := SaveNodes(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadNodes()
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if err := SaveNodes(nil); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadNodes(); got != nil || err != nil {
		t.Errorf("removing the last node should leave no config, got %+v, %v", got, err)
	}
}

func TestParseNodesRejectsBadPort(t *testing.T) {
	for _, bad := range []string{
		`{"nodes":[{"name":"a","host":"h","port":0}]}`,
		`{"nodes":[{"name":"a","host":"h","port":70000}]}`,
		`{"nodes":[{"name":"a","port":22}]}`,
	} {
		if _, err := parseNodes([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestSSHArgsCarryPortAndOnlyAddingTrustsNewKeys(t *testing.T) {
	plain := strings.Join(sshArgs(Target{Host: "h", Port: 8022}, "cmd"), " ")
	if !strings.Contains(plain, "-p 8022") {
		t.Errorf("port dropped: %s", plain)
	}
	if strings.Contains(plain, "accept-new") {
		t.Errorf("routine commands must not trust new host keys: %s", plain)
	}
	if !strings.Contains(strings.Join(sshArgs(Target{Host: "h", trustNewKey: true}, "cmd"), " "), "accept-new") {
		t.Error("adding a device should accept a new key")
	}
}

func TestParseProbeOrdersServersAndDropsBadNames(t *testing.T) {
	p := parseProbe("os=Darwin\ntmux=/opt/homebrew/bin/tmux\nserver=vibe\nserver=default\nserver=vibe\nserver=a b\n")
	if p.OS != "Darwin" || !p.HasTmux || !slices.Equal(p.Servers, []string{"default", "vibe"}) {
		t.Errorf("got %+v", p)
	}
	if p := parseProbe("os=Darwin\nstray=/opt/homebrew/bin/tmux\n"); p.HasTmux || p.StrayTmux == "" {
		t.Errorf("got %+v", p)
	}
}

func TestProbeFindsOnlyLiveServers(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Short, because a unix socket's path has a low length limit.
	dir, err := os.MkdirTemp("", "fl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	for _, name := range []string{"live", "dead"} {
		if out, err := exec.Command("tmux", "-L", name, "-f", "/dev/null", "new-session", "-d", "sleep 60").CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", err, out)
		}
	}
	defer exec.Command("tmux", "-L", "live", "kill-server").Run()
	exec.Command("tmux", "-L", "dead", "kill-server").Run()
	p, err := ProbeTarget("", 0)
	if err != nil || !p.HasTmux {
		t.Fatalf("%+v %v", p, err)
	}
	if !slices.Contains(p.Servers, "live") || slices.Contains(p.Servers, "dead") {
		t.Errorf("servers = %v", p.Servers)
	}
}

func TestParsePeers(t *testing.T) {
	peers, err := parsePeers([]byte(`{"Peer":{
		"a":{"HostName":"archlinux","DNSName":"archlinux-1.x.ts.net.","OS":"linux","TailscaleIPs":["100.1.1.1","fd7a::1"],"Online":false},
		"b":{"HostName":"Jeffs-Mac-mini","DNSName":"jeffs-mac-mini.x.ts.net.","OS":"macOS","TailscaleIPs":["100.2.2.2"],"Online":true},
		"c":{"HostName":"none","DNSName":"","OS":"linux","TailscaleIPs":[],"Online":true}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Peer{
		{Name: "jeffs-mac-mini", OS: "macOS", IP: "100.2.2.2", Online: true},
		{Name: "archlinux-1", OS: "linux", IP: "100.1.1.1"},
	}
	if !slices.Equal(peers, want) {
		t.Errorf("got %+v", peers)
	}
}

func TestSelfAddress(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-0/vibe,123,0")
	t.Setenv("TMUX_PANE", "%26")
	if got := SelfAddress(Node{Name: "mac"}); got != "./vibe/%26" {
		t.Errorf("local: %q", got)
	}
	if got := SelfAddress(Node{Host: "phone", ReturnHost: "macmini"}); got != "macmini/vibe/%26" {
		t.Errorf("remote: %q", got)
	}
	if _, _, _, err := ParseAddress(SelfAddress(Node{Host: "phone"})); err == nil {
		t.Error("remote without return_host must not produce a usable address")
	}
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	if got := SelfAddress(Node{Name: "mac"}); got != "" {
		t.Errorf("outside tmux: %q", got)
	}
}

func TestVisibleStripsTerminalControl(t *testing.T) {
	if got := Visible("a\x1b[2Jb\x07c​d\t手"); strings.ContainsAny(got, "\x1b\x07​\t") || !strings.Contains(got, "手") {
		t.Errorf("%q", got)
	}
	if got := VisibleBody("one\r\ntwo\x1b"); got != "one\ntwo " {
		t.Errorf("%q", got)
	}
}
