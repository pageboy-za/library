package metrics

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

func TestTracePreservesCorrelationWithoutLoggingErrorContents(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	ctx, trace := Start(context.Background())
	ctx = context.WithoutCancel(ctx)
	Record(ctx, "database", 12*time.Millisecond, nil)
	err := Measure(ctx, "cover", func() error { return errors.New("private fixture notes and password") })
	if err == nil {
		t.Fatal("error discarded")
	}
	trace.Finish()
	if strings.Contains(output.String(), "private fixture") || strings.Contains(output.String(), "password") {
		t.Fatal("private error text logged")
	}
	if !strings.Contains(output.String(), trace.ID) || !strings.Contains(trace.Header(), "database;dur=12.00") {
		t.Fatal("missing trace correlation/timing")
	}
}
