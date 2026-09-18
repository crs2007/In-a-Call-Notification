package mqtt

import (
	"context"
	"testing"
	"time"
)

// TestPublishTimeoutConstant pins the 5s bound from TODO 5.3: the engine's
// poll loop can never stall more than one tick-plus-publishTimeout on a
// half-open socket. If this drifts, docs/mqtt.md and the reasoning in
// client.go's comments drift with it, so change it deliberately.
func TestPublishTimeoutConstant(t *testing.T) {
	if publishTimeout != 5*time.Second {
		t.Errorf("publishTimeout = %s, want 5s", publishTimeout)
	}
}

// TestBoundedPublishContextBoundsAnUnboundedCtx is the regression test for
// TODO 5.3: a ctx that would otherwise block forever (context.Background(),
// against a broker that never acks) must come back with a deadline instead
// of none. There's no fake broker in this package to actually hang a publish
// against, so this asserts the thing that makes hanging impossible in the
// first place: publish() never uses the caller's ctx unbounded, it always
// derives a child ctx with a deadline no later than now+publishTimeout.
func TestBoundedPublishContextBoundsAnUnboundedCtx(t *testing.T) {
	before := time.Now()

	ctx, cancel := boundedPublishContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("boundedPublishContext(context.Background()) has no deadline; a stuck PUBACK would hang forever")
	}

	after := time.Now()

	if deadline.Before(before.Add(publishTimeout)) {
		t.Errorf("deadline %s is more than publishTimeout early relative to call start %s", deadline, before)
	}
	if deadline.After(after.Add(publishTimeout)) {
		t.Errorf("deadline %s is later than publishTimeout after call end %s; caller ctx was not bounded", deadline, after)
	}
}

// TestBoundedPublishContextNeverLoosensACallersDeadline confirms the bound is
// additive, not a replacement: a caller that already passed a tighter
// deadline (e.g. a request-scoped ctx) must keep it. publishTimeout should
// only ever shorten an absent or overly generous deadline, never lengthen one
// the caller already set.
func TestBoundedPublishContextNeverLoosensACallersDeadline(t *testing.T) {
	tight := 50 * time.Millisecond
	parent, parentCancel := context.WithTimeout(context.Background(), tight)
	defer parentCancel()

	before := time.Now()
	ctx, cancel := boundedPublishContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("boundedPublishContext lost the parent's deadline entirely")
	}
	if deadline.After(before.Add(tight + 10*time.Millisecond)) {
		t.Errorf("deadline %s did not respect the caller's tighter %s deadline", deadline, tight)
	}

	select {
	case <-ctx.Done():
		// Expected: the caller's own tight deadline fired.
	case <-time.After(publishTimeout):
		t.Error("ctx did not expire on the caller's tighter deadline within publishTimeout")
	}
}
