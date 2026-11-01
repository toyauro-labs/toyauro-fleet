package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/toyauro-labs/toyauro-fleet/packages/fleet/internal/fleet"
)

func init() {
	var all, asJSON bool
	list := &cobra.Command{
		Use:   "list",
		Short: "Discover configured fleet panes and their adon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodes, err := selectedNodes(cmd)
			if err != nil {
				return err
			}
			panes, errs := fleet.Discover(nodes, all)
			if asJSON {
				if err := printJSON(map[string]any{"panes": panes, "errors": errs}, "  "); err != nil {
					return err
				}
			} else {
				printTable(panes, terminalColumns(), useColor())
				for _, e := range errs {
					fmt.Fprintf(os.Stderr, "%s / %s: UNAVAILABLE: %s\n",
						fleet.Visible(e.Device), fleet.Visible(e.Server), fleet.Visible(e.Error))
				}
			}
			if len(errs) > 0 {
				return errSilent
			}
			return nil
		},
	}
	list.Flags().BoolVar(&all, "all", false, "include unnamed, unconnected panes")
	list.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	rootCmd.AddCommand(list)
}

func terminalColumns() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
		return n
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

func useColor() bool {
	_, noColor := os.LookupEnv("NO_COLOR")
	return term.IsTerminal(int(os.Stdout.Fd())) && !noColor && os.Getenv("TERM") != "dumb"
}

var stateColor = map[string]string{"working": "92", "waiting": "93", "error": "91", "idle": "97"}

// lamp is the status glyph; it is the only part of the label ever colored, so
// the plain label measures the same width.
func lamp(state string, color bool) string {
	glyph := fleet.Glyph(state)
	if code, ok := stateColor[state]; ok && color {
		return "\033[" + code + "m" + glyph + "\033[0m"
	}
	return glyph
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func wrapCells(s string, width int) []string {
	var lines []string
	line := ""
	for _, r := range s {
		if line != "" && ansi.StringWidth(line+string(r)) > width {
			lines = append(lines, line)
			line = ""
		}
		line += string(r)
	}
	return append(lines, line)
}

type record struct{ device, server, name, state, command, location, pane string }

func (r record) cells() []string {
	return []string{r.device, r.server, r.name, fleet.Glyph(r.state) + " " + r.state, r.command, r.location, r.pane}
}

func (r record) group() string { return r.device + "\x00" + r.server }

// printTable writes the view for scripts and piped use: a grid when it fits,
// otherwise a card per device for narrow phone terminals.
func printTable(panes []fleet.DevicePane, columns int, color bool) {
	records := make([]record, len(panes))
	for i, p := range panes {
		name, state := p.Name, p.State
		if name == "" {
			name = "—"
		}
		if state == "" {
			state = "unknown"
		}
		records[i] = record{fleet.Visible(p.Device), fleet.Visible(p.Server), fleet.Visible(name),
			fleet.Visible(state), fleet.Visible(p.Command), fleet.Visible(p.Location()), fleet.Visible(p.Pane.Pane)}
	}
	headers := []string{"DEVICE", "SERVER", "WORKSTATION", "STATUS", "PROCESS", "SESSION:WIN.PANE", "ID"}
	widths := make([]int, len(headers))
	total := 1
	for i, h := range headers {
		widths[i] = ansi.StringWidth(h)
		for _, r := range records {
			widths[i] = max(widths[i], ansi.StringWidth(r.cells()[i]))
		}
		total += widths[i] + 3
	}
	switch frame := min(columns, 64); {
	case total <= columns:
		printGrid(headers, widths, records, color)
	case frame < 12:
		for _, r := range records {
			text := strings.Join([]string{r.device, r.server, r.name, r.state, r.location, r.pane, r.command}, " / ")
			for _, line := range wrapCells(text, max(1, columns)) {
				fmt.Println(line)
			}
		}
	default:
		printCards(records, frame, color)
	}
}

func printGrid(headers []string, widths []int, records []record, color bool) {
	rule := func(left, middle, right string) {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		fmt.Println(left + strings.Join(parts, middle) + right)
	}
	line := func(cells []string) {
		parts := make([]string, len(cells))
		for i, cell := range cells {
			parts[i] = pad(cell, widths[i])
		}
		fmt.Println("│ " + strings.Join(parts, " │ ") + " │")
	}
	rule("╭", "┬", "╮")
	line(headers)
	rule("├", "┼", "┤")
	for i, r := range records {
		if i > 0 && r.group() != records[i-1].group() {
			rule("├", "┼", "┤")
		}
		cells := r.cells()
		padded := pad(cells[3], widths[3])
		cells[3] = lamp(r.state, color) + strings.TrimPrefix(padded, fleet.Glyph(r.state))
		// Width of a colored cell is already right: pad counts escapes as zero.
		line(cells)
	}
	rule("╰", "┴", "╯")
}

func printCards(records []record, frame int, color bool) {
	inner := frame - 4
	rule := func(left, right string) { fmt.Println(left + strings.Repeat("─", frame-2) + right) }
	framed := func(s string) {
		fmt.Println("│ " + pad(s, inner) + " │")
	}
	for i, r := range records {
		if i == 0 || r.group() != records[i-1].group() {
			if i > 0 {
				rule("╰", "╯")
				fmt.Println()
			}
			rule("╭", "╮")
			for _, line := range wrapCells(r.device+"  /  "+r.server, inner) {
				framed(line)
			}
			rule("├", "┤")
		}
		glyph := fleet.Glyph(r.state)
		for j, line := range wrapCells(glyph+" "+r.name+"  ·  "+r.state, inner) {
			if j == 0 {
				line = lamp(r.state, color) + strings.TrimPrefix(line, glyph)
			}
			framed(line)
		}
		for _, line := range wrapCells("  "+r.location+"  "+r.pane+"  ·  "+r.command, inner) {
			framed(line)
		}
	}
	if len(records) == 0 {
		fmt.Println("No workstations found.")
		return
	}
	rule("╰", "╯")
}
