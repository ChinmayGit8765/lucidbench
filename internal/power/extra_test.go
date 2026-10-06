package power

import (
	"context"
	"testing"
)

type fakeExtra struct{ ticks, sleeps int }

func (f *fakeExtra) Tick(context.Context) { f.ticks++ }
func (f *fakeExtra) Sleep(context.Context) []Outcome {
	f.sleeps++
	return []Outcome{{Kind: "manager", Name: "lucidbench-mgr-x", Stopped: true, Reason: "stopped"}}
}

func TestExtrasAreTickedAndSlept(t *testing.T) {
	r := newRig(t, powerCfg("off", "off"))
	x := &fakeExtra{}
	r.s.AddExtra(x)
	r.s.Tick(ctx)
	if x.ticks != 1 {
		t.Errorf("ticks = %d", x.ticks)
	}
	out := r.s.SleepIdle(ctx)
	if x.sleeps != 1 || len(out) == 0 || out[len(out)-1].Kind != "manager" || !out[len(out)-1].Stopped {
		t.Errorf("sleep = %d, outcomes = %+v", x.sleeps, out)
	}
}
