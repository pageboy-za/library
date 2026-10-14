// Package metrics records save phases without catalogue or credential values.
package metrics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

type key struct{}
type Trace struct {
	ID     string
	start  time.Time
	mu     sync.Mutex
	stages map[string]time.Duration
}

func Start(ctx context.Context) (context.Context, *Trace) {
	var b [12]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	t := &Trace{ID: hex.EncodeToString(b[:]), start: time.Now(), stages: map[string]time.Duration{}}
	return context.WithValue(ctx, key{}, t), t
}
func Get(ctx context.Context) *Trace { t, _ := ctx.Value(key{}).(*Trace); return t }
func Record(ctx context.Context, stage string, elapsed time.Duration, err error) {
	if t := Get(ctx); t != nil {
		t.mu.Lock()
		t.stages[stage] += elapsed
		t.mu.Unlock()
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		log.Printf("save_timing request=%s stage=%s duration_ms=%.2f outcome=%s", t.ID, stage, float64(elapsed)/float64(time.Millisecond), outcome)
	}
}
func Measure(ctx context.Context, stage string, fn func() error) error {
	start := time.Now()
	err := fn()
	Record(ctx, stage, time.Since(start), err)
	return err
}
func (t *Trace) Header() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := []string{}
	for n := range t.stages {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := []string{}
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s;dur=%.2f", n, float64(t.stages[n])/float64(time.Millisecond)))
	}
	return strings.Join(parts, ", ")
}
func (t *Trace) Finish() {
	log.Printf("save_timing request=%s stage=request_total duration_ms=%.2f", t.ID, float64(time.Since(t.start))/float64(time.Millisecond))
}
