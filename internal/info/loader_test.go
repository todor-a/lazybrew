package info

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"lazybrew/internal/brew"
)

type loadReply struct {
	text string
	err  error
}

type loadCall struct {
	pkg   brew.Package
	reply chan loadReply
}

type controlledLoad struct {
	calls chan loadCall
}

func newControlledLoader() (*controlledLoad, *Loader) {
	controlled := &controlledLoad{calls: make(chan loadCall, 1)}
	return controlled, New(controlled.load)
}

func (c *controlledLoad) load(ctx context.Context, pkg brew.Package) (string, error) {
	call := loadCall{pkg: pkg, reply: make(chan loadReply, 1)}
	select {
	case c.calls <- call:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case reply := <-call.reply:
		return reply.text, reply.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func startCommand(t *testing.T, cmd tea.Cmd) <-chan Result {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected info command")
	}

	messages := make(chan Result, 1)
	go func() {
		msg := cmd()
		result, ok := msg.(Result)
		if !ok {
			panic("info command returned an unexpected message type")
		}
		messages <- result
	}()

	return messages
}

func nextCall(t *testing.T, controlled *controlledLoad) loadCall {
	t.Helper()
	select {
	case call := <-controlled.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("info command did not call the loader")
		return loadCall{}
	}
}

func nextResult(t *testing.T, results <-chan Result) Result {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("info command did not complete")
		return Result{}
	}
}

func pkg(name string) brew.Package {
	return brew.Package{Name: name, Kind: brew.Cask}
}

func TestLoaderKeepsOnlyLatestPendingAndCachesCompletedTargets(t *testing.T) {
	controlled, loader := newControlledLoader()
	a, b, c := pkg("a"), pkg("b"), pkg("c")

	resultsA := startCommand(t, loader.Select(&a))
	callA := nextCall(t, controlled)
	if callA.pkg != a {
		t.Fatalf("first load = %#v, want %#v", callA.pkg, a)
	}
	if cmd := loader.Select(&b); cmd != nil {
		t.Fatal("B started while A was active")
	}
	if cmd := loader.Select(&c); cmd != nil {
		t.Fatal("C started while A was active")
	}

	callA.reply <- loadReply{text: "A info\n"}
	cmdC := loader.Complete(nextResult(t, resultsA))
	if got := loader.Text(); got != loadingText {
		t.Fatalf("text after non-current A completion = %q, want %q", got, loadingText)
	}

	resultsC := startCommand(t, cmdC)
	callC := nextCall(t, controlled)
	if callC.pkg != c {
		t.Fatalf("pending load = %#v, want latest %#v", callC.pkg, c)
	}
	callC.reply <- loadReply{text: "C info\r\n"}
	if cmd := loader.Complete(nextResult(t, resultsC)); cmd != nil {
		t.Fatal("idle completion scheduled another command")
	}
	if got := loader.Text(); got != "C info" {
		t.Fatalf("current text = %q, want trimmed C info", got)
	}

	if cmd := loader.Select(&a); cmd != nil {
		t.Fatal("revisiting cached A scheduled a duplicate command")
	}
	if got := loader.Text(); got != "A info" {
		t.Fatalf("cached A text = %q, want %q", got, "A info")
	}
}

func TestLoaderActiveReselectClearsPendingAfterProcessCompletes(t *testing.T) {
	controlled, loader := newControlledLoader()
	a, b := pkg("a"), pkg("b")

	resultsA := startCommand(t, loader.Select(&a))
	callA := nextCall(t, controlled)
	callA.reply <- loadReply{text: "A info"}
	resultA := nextResult(t, resultsA)

	if cmd := loader.Select(&b); cmd != nil {
		t.Fatal("B started before A result was handled")
	}
	if cmd := loader.Select(&a); cmd != nil {
		t.Fatal("completed-but-unhandled A was started twice")
	}
	if cmd := loader.Complete(resultA); cmd != nil {
		t.Fatal("A -> B -> A reselect did not clear pending B")
	}
	if got := loader.Text(); got != "A info" {
		t.Fatalf("text = %q, want completed A", got)
	}
}

func TestLoaderRefreshDiscardsOldGeneration(t *testing.T) {
	controlled, loader := newControlledLoader()
	a := pkg("a")

	oldResults := startCommand(t, loader.Select(&a))
	oldCall := nextCall(t, controlled)
	if cmd := loader.Refresh(&a); cmd != nil {
		t.Fatal("refresh started a second command while the old generation was active")
	}

	oldCall.reply <- loadReply{text: "stale info"}
	freshCmd := loader.Complete(nextResult(t, oldResults))
	if got := loader.Text(); got != loadingText {
		t.Fatalf("old generation rendered %q", got)
	}
	if cmd := loader.Select(&a); cmd != nil {
		t.Fatal("reselect started a duplicate fresh-generation command")
	}
	if got := loader.Text(); got != loadingText {
		t.Fatalf("old generation entered cache: text = %q", got)
	}

	freshResults := startCommand(t, freshCmd)
	freshCall := nextCall(t, controlled)
	freshCall.reply <- loadReply{text: "fresh info"}
	if cmd := loader.Complete(nextResult(t, freshResults)); cmd != nil {
		t.Fatal("fresh completion scheduled another command")
	}
	if got := loader.Text(); got != "fresh info" {
		t.Fatalf("text = %q, want fresh generation", got)
	}
}

func TestLoaderCompletionRendersOnlyCurrentTarget(t *testing.T) {
	controlled, loader := newControlledLoader()
	a := pkg("a")

	results := startCommand(t, loader.Select(&a))
	call := nextCall(t, controlled)
	if cmd := loader.Select(nil); cmd != nil {
		t.Fatal("clearing selection scheduled a command")
	}
	call.reply <- loadReply{text: "A info"}
	if cmd := loader.Complete(nextResult(t, results)); cmd != nil {
		t.Fatal("completion with no target scheduled a command")
	}
	if got := loader.Text(); got != "" {
		t.Fatalf("non-current completion rendered %q", got)
	}

	if cmd := loader.Select(&a); cmd != nil {
		t.Fatal("cached non-current completion was loaded again")
	}
	if got := loader.Text(); got != "A info" {
		t.Fatalf("cached completion = %q, want A info", got)
	}
}

func TestLoaderIdleAndCancellationHooks(t *testing.T) {
	controlled, loader := newControlledLoader()
	select {
	case <-loader.Done():
	default:
		t.Fatal("new loader is not idle")
	}
	if cmd := loader.Select(nil); cmd != nil {
		t.Fatal("nil selection scheduled a command")
	}
	if cmd := loader.Refresh(nil); cmd != nil {
		t.Fatal("nil refresh scheduled a command")
	}
	if cmd := loader.Complete(Result{}); cmd != nil {
		t.Fatal("unexpected result scheduled a command")
	}

	a := pkg("a")
	results := startCommand(t, loader.Select(&a))
	_ = nextCall(t, controlled)
	loader.Cancel()
	result := nextResult(t, results)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cancelled load error = %v, want context cancellation", result.Err)
	}
	select {
	case <-loader.Done():
	case <-time.After(time.Second):
		t.Fatal("Done did not close after cancelled load returned")
	}
	if cmd := loader.Complete(result); cmd != nil {
		t.Fatal("cancelled completion scheduled another command")
	}
	if cmd := loader.Select(&a); cmd != nil {
		t.Fatal("cancelled loader accepted new work")
	}
}
