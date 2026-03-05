package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	successMark = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓")
	warnMark    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("⚠")
	failMark    = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("✗")
	bulletMark  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("●")
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Display struct {
	out io.Writer
	mu  sync.Mutex
}

func New() *Display {
	return &Display{out: os.Stderr}
}

func (d *Display) Title(owner, repo string, number int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "\n%s  %s  %s/%s#%d\n\n",
		bulletMark,
		titleStyle.Render("Start Fleet"),
		owner, repo, number)
}

func (d *Display) Step(label, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %-22s %s  %s\n", label, successMark, detail)
}

func (d *Display) StepFail(label, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %-22s %s  %s\n", label, failMark, detail)
}

func (d *Display) StepWarn(label, detail string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %-22s %s  %s\n", label, warnMark, detail)
}

type Spinner struct {
	d       *Display
	prefix  string
	tree    string
	stopCh  chan struct{}
	doneCh  chan struct{}
}

func (d *Display) TreeBranch(label, message string) *Spinner {
	return d.startSpinner("├", label, message)
}

func (d *Display) TreeLeaf(label, message string) *Spinner {
	return d.startSpinner("└", label, message)
}

func (d *Display) startSpinner(tree, label, message string) *Spinner {
	s := &Spinner{
		d:      d,
		prefix: label,
		tree:   tree,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go func() {
		defer close(s.doneCh)
		i := 0
		for {
			select {
			case <-s.stopCh:
				return
			default:
				frame := spinnerFrames[i%len(spinnerFrames)]
				s.d.mu.Lock()
				fmt.Fprintf(s.d.out, "\r  %s %s%s  %s",
					s.tree,
					lipgloss.NewStyle().Width(20).Render(s.prefix),
					lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(frame),
					dimStyle.Render(message))
				s.d.mu.Unlock()
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
	return s
}

func (s *Spinner) Stop(status, detail string) {
	close(s.stopCh)
	<-s.doneCh

	var mark string
	switch status {
	case "success":
		mark = successMark
	case "warn":
		mark = warnMark
	case "fail":
		mark = failMark
	default:
		mark = successMark
	}

	s.d.mu.Lock()
	defer s.d.mu.Unlock()
	fmt.Fprintf(s.d.out, "\r  %s %s%s  %s\n",
		s.tree,
		lipgloss.NewStyle().Width(20).Render(s.prefix),
		mark,
		detail)
}

func (d *Display) Blank() {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintln(d.out)
}

func (d *Display) Info(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %s\n", msg)
}

func (d *Display) Success(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %s  %s\n", successMark, msg)
}

func (d *Display) Warn(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %s  %s\n", warnMark, msg)
}

func (d *Display) Fail(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "  %s  %s\n", failMark, msg)
}

func (d *Display) Result(url string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "\n  %s  PR ready for review\n     → %s\n\n",
		successMark, url)
}

func (d *Display) FailResult(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.out, "\n  %s  %s\n\n", failMark, msg)
}
