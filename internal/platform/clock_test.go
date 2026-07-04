package platform

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRealClockImplementsClock(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	var clock Clock = RealClock{}
	now := clock.Now()

	r.WithinDuration(time.Now(), now, time.Second)
	r.GreaterOrEqual(clock.Since(now), time.Duration(0))
}

func TestClockCanBeInjectedDeterministically(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: now}

	r.Equal(now, clock.Now())
	r.Equal(5*time.Second, clock.Since(now.Add(-5*time.Second)))
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func (c fixedClock) Since(t time.Time) time.Duration {
	return c.now.Sub(t)
}
