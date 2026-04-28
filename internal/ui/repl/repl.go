// Package repl provides an interactive Read-Eval-Print Loop that drives an
// app.Session from stdin/stdout. All I/O goes through the io.Reader/io.Writer
// arguments so the loop can be tested headlessly.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/model"
)

const banner = `FEINO — AI agent CLI
Type :help for commands, :quit to exit.
`

const helpText = `Commands:
  :help     show this message
  :quit :q  exit
  :reset    clear conversation history
  :history  print conversation so far
  :config   print active configuration as YAML

Anything else is sent to the model.
`

const prompt = "FEINO> "

// safeWriter serialises all Write calls so the subscriber goroutine and the
// SIGINT handler goroutine can write concurrently without a data race.
type safeWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *safeWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func (s *safeWriter) WriteString(str string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return io.WriteString(s.w, str)
}

// Run starts the interactive REPL. It reads from in and writes to out,
// blocking until the user quits, in reaches EOF, or ctx is cancelled.
// SIGINT cancels the current in-flight turn; a second SIGINT (when idle) prints a hint.
func Run(ctx context.Context, sess *app.Session, in io.Reader, out io.Writer) error {
	sw := &safeWriter{w: out}

	_, _ = sw.WriteString(banner)

	// Handle SIGINT: cancel in-flight turn; print hint when idle.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer func() {
		signal.Stop(sigCh)
		close(sigCh) // unblocks the goroutine so it can exit
	}()
	go func() {
		for range sigCh {
			if sess.GetCurrentState() != agent.StateInit {
				_, _ = fmt.Fprintln(sw, "\n[cancelled]")
				sess.Cancel()
			} else {
				_, _ = fmt.Fprintln(sw, "\n(type :quit to exit)")
			}
		}
	}()

	// Register a single subscriber for the lifetime of Run.
	// currentDone stores the per-turn completion channel (a reference type);
	// it is swapped atomically before each Send so the subscriber always
	// signals the correct turn.
	var currentDone atomic.Value // stores chan struct{}
	sess.Subscribe(func(e app.Event) {
		switch e.Kind {
		case app.EventPartReceived:
			if part, ok := e.Payload.(model.MessagePart); ok {
				if _, isThought := part.(*model.ThoughtPart); isThought {
					return // suppress internal reasoning
				}
				if text, ok := part.GetContent().(string); ok {
					_, _ = fmt.Fprint(sw, text)
				}
			}
		case app.EventStateChanged:
			if state, ok := e.Payload.(agent.ReActState); ok && state == agent.StateAct {
				_, _ = fmt.Fprintln(sw, "\n  [thinking...]")
			}
		case app.EventError:
			_, _ = fmt.Fprintf(sw, "\nerror: %v\n", e.Payload)
		case app.EventComplete:
			_, _ = fmt.Fprintln(sw)
			if ch, ok := currentDone.Load().(chan struct{}); ok {
				close(ch)
			}
		}
	})

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), 1<<20) // 1 MB limit

	for {
		_, _ = sw.WriteString(prompt)
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch line {
		case ":quit", ":q":
			return nil
		case ":reset":
			_ = sess.Reset()
			_, _ = fmt.Fprintln(sw, "conversation reset")
			continue
		case ":history":
			printHistory(sw, sess.History())
			continue
		case ":help":
			_, _ = sw.WriteString(helpText)
			continue
		case ":config":
			printConfig(sw, sess)
			continue
		}

		// Send the user message and wait for the turn to complete.
		done := make(chan struct{})
		currentDone.Store(done)

		if err := sess.Send(ctx, line); err != nil {
			_, _ = fmt.Fprintf(sw, "error: %v\n", err)
			continue
		}

		select {
		case <-done:
		case <-ctx.Done():
			return nil
		}
	}

	return scanner.Err()
}

func printHistory(out io.Writer, history []model.Message) {
	if len(history) == 0 {
		_, _ = fmt.Fprintln(out, "(no history)")
		return
	}
	for _, msg := range history {
		_, _ = fmt.Fprintf(out, "[%s] %s\n", msg.GetRole(), msg.GetTextContent())
	}
}

func printConfig(out io.Writer, sess *app.Session) {
	data, err := yaml.Marshal(sess.Config())
	if err != nil {
		_, _ = fmt.Fprintf(out, "error marshalling config: %v\n", err)
		return
	}
	_, _ = out.Write(data)
}
