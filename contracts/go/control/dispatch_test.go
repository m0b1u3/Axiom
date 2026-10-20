package control

import "testing"

func TestDispatchPresenceRequiresExplicitUnboundedMode(t *testing.T) {
	for _, v := range []struct {
		mode  string
		slots int
		want  bool
	}{{DispatchUnbounded, 0, true}, {"", 0, false}, {DispatchBounded, 0, false}, {"future", 0, false}, {DispatchUnbounded, 2, false}, {"", 2, true}, {DispatchBounded, 64, true}, {DispatchBounded, 65, true}, {DispatchBounded, 9007199254740991, true}, {DispatchBounded, 9007199254740992, false}, {DispatchUnbounded, -1, false}} {
		if ValidDispatchPresence(v.mode, v.slots) != v.want {
			t.Fatalf("dispatch mode %q slots %d", v.mode, v.slots)
		}
	}
}
