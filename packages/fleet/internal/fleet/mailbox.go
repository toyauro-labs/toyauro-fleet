package fleet

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	// Prefix marks a tmux buffer as a fleet message; the rest of the name is its ID.
	Prefix  = "fleet-msg-"
	MaxBody = 128 * 1024

	messageVersion = 1
)

var messageIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Message is the wire format stored in a buffer. Every device reads what any
// other wrote, so fields can only be added compatibly.
type Message struct {
	Version int     `json:"version"`
	ID      string  `json:"id"`
	Thread  string  `json:"thread"`
	ReplyTo *string `json:"reply_to"`
	From    string  `json:"from"`
	ToPane  string  `json:"to_pane"`
	Created string  `json:"created"`
	Body    string  `json:"body"`
}

// Receipt reports that a message reached the target's tmux. It says nothing
// about whether anyone has read it.
type Receipt struct {
	Stored bool   `json:"stored"`
	ID     string `json:"id"`
	Thread string `json:"thread"`
	ToPane string `json:"to_pane"`
	Host   string `json:"host"`
	Server string `json:"server"`
}

// Marshal encodes without HTML escaping, leaving non-ASCII text readable.
func Marshal(v any, indent string) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// CheckPane rejects anything but a tmux pane ID such as %2.
func CheckPane(pane string) error {
	if !paneRe.MatchString(pane) {
		return errors.New("pane must be a tmux pane ID such as %2")
	}
	return nil
}

func checkID(id string) error {
	if !messageIDRe.MatchString(id) {
		return errors.New("invalid message ID")
	}
	return nil
}

// ParseAddress splits a return address, HOST/SERVER/%PANE. HOST is an SSH alias
// as seen from the recipient's device, or "." for that same device.
func ParseAddress(addr string) (host, server, pane string, err error) {
	parts := strings.Split(addr, "/")
	if len(parts) != 3 || parts[0] == "" || !ValidServer(parts[1]) {
		return "", "", "", errors.New("address must be HOST/SERVER/%PANE (HOST is an SSH alias reachable by the peer)")
	}
	if err := CheckPane(parts[2]); err != nil {
		return "", "", "", err
	}
	return parts[0], parts[1], parts[2], nil
}

// ReadBody takes the --body value, reading stdin for "-".
func ReadBody(value string, stdin io.Reader) (string, error) {
	if value != "-" {
		return value, CheckBody(value)
	}
	data, err := io.ReadAll(io.LimitReader(stdin, MaxBody+1))
	if err != nil {
		return "", err
	}
	return string(data), CheckBody(string(data))
}

// CheckBody rejects bodies that are blank or too large to store.
func CheckBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("message body is empty")
	}
	if len(body) > MaxBody {
		return errors.New("message body exceeds 128 KiB")
	}
	return nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80 // UUID v4
	return hex.EncodeToString(b[:]), nil
}

func (t Target) paneExists(pane string) error {
	out, err := t.run(t.tmux("list-panes", "-a", "-F", "#{pane_id}"), "")
	if err != nil {
		return err
	}
	if !slices.Contains(strings.Split(out, "\n"), pane) {
		return errors.New("pane does not exist on the selected tmux server: " + pane)
	}
	return nil
}

// Send stores a new message for a pane.
func Send(t Target, to, from, body string) (Receipt, error) {
	return send(t, to, from, body, nil, "")
}

func send(t Target, to, from, body string, replyTo *string, thread string) (Receipt, error) {
	if err := CheckPane(to); err != nil {
		return Receipt{}, err
	}
	if _, _, _, err := ParseAddress(from); err != nil {
		return Receipt{}, err
	}
	if err := CheckBody(body); err != nil {
		return Receipt{}, err
	}
	if err := t.paneExists(to); err != nil {
		return Receipt{}, err
	}
	id, err := newID()
	if err != nil {
		return Receipt{}, err
	}
	if thread == "" {
		thread = id
	}
	data, err := Marshal(Message{
		Version: messageVersion, ID: id, Thread: thread, ReplyTo: replyTo, From: from, ToPane: to,
		Created: time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00"), Body: body,
	}, "")
	if err != nil {
		return Receipt{}, err
	}
	if _, err := t.run(t.tmux("load-buffer", "-b", Prefix+id, "-"), string(data)); err != nil {
		return Receipt{}, err
	}
	return Receipt{Stored: true, ID: id, Thread: thread, ToPane: to, Host: t.HostLabel(), Server: t.Server}, nil
}

func decode(id, data string) (Message, error) {
	var m Message
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		return m, errors.New("message buffer is not valid JSON")
	}
	if m.ID != id || m.Version != messageVersion {
		return m, errors.New("invalid fleet message")
	}
	return m, nil
}

func (t Target) get(id string) (Message, error) {
	if err := checkID(id); err != nil {
		return Message{}, err
	}
	out, _, err := t.exec(t.tmux("show-buffer", "-b", Prefix+id), "")
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return Message{}, fmt.Errorf("tmux/SSH unavailable: %w", err)
		}
		return Message{}, errors.New("message not found on the selected server: " + id)
	}
	return decode(id, out)
}

// Messages reads every message on the server, oldest first. It reads them in one
// remote shell, so a full inbox costs one SSH connection rather than one each.
func Messages(t Target) ([]Message, error) {
	tmux := shellJoin(t.tmux())
	script := "for b in $(" + tmux + " list-buffers -F '#{buffer_name}' | grep -E '^" + Prefix + "[0-9a-f]{32}$'); do " +
		"printf '%s\\t' \"$b\"; " + tmux + " show-buffer -b \"$b\"; echo; done"
	out, err := t.run([]string{"sh", "-c", script}, "")
	if err != nil {
		return nil, err
	}
	return parseMessages(out), nil
}

// parseMessages reads "NAME<TAB>JSON" lines. Encoded messages contain no raw
// newlines; a buffer that fails to parse, such as one acknowledged while being
// read, is not a message anyone can act on, so it is skipped.
func parseMessages(out string) []Message {
	messages := []Message{}
	for _, line := range strings.Split(out, "\n") {
		name, data, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(name, Prefix) {
			continue
		}
		if m, err := decode(strings.TrimPrefix(name, Prefix), data); err == nil {
			messages = append(messages, m)
		}
	}
	slices.SortFunc(messages, func(a, b Message) int {
		if c := strings.Compare(a.Created, b.Created); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return messages
}

// Inbox reads a pane's messages without removing them.
func Inbox(t Target, pane string) ([]Message, error) {
	if err := CheckPane(pane); err != nil {
		return nil, err
	}
	if err := t.paneExists(pane); err != nil {
		return nil, err
	}
	all, err := Messages(t)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(m Message) bool { return m.ToPane != pane }), nil
}

func (t Target) owned(pane, id string) (Message, error) {
	if err := CheckPane(pane); err != nil {
		return Message{}, err
	}
	if err := t.paneExists(pane); err != nil {
		return Message{}, err
	}
	m, err := t.get(id)
	if err != nil {
		return Message{}, err
	}
	if m.ToPane != pane {
		return Message{}, errors.New("message belongs to another pane")
	}
	return m, nil
}

// Ack deletes one processed message from a pane's inbox.
func Ack(t Target, pane, id string) error {
	if _, err := t.owned(pane, id); err != nil {
		return err
	}
	_, err := t.run(t.tmux("delete-buffer", "-b", Prefix+id), "")
	return err
}

// Reply answers a message at its return address, keeping its thread. The
// original stays in the inbox until acknowledged.
func Reply(t Target, pane, id, from, body string) (Receipt, error) {
	m, err := t.owned(pane, id)
	if err != nil {
		return Receipt{}, err
	}
	host, server, to, err := ParseAddress(m.From)
	if err != nil {
		return Receipt{}, err
	}
	return send(replyTarget(t, host, server), to, from, body, &m.ID, m.Thread)
}

// replyTarget resolves a return address read from t's mailbox. "." means the
// device holding the mailbox, which is not this machine under --host.
func replyTarget(t Target, host, server string) Target {
	if host == "." {
		host = t.Host
	}
	return Target{Host: host, Server: server}
}
