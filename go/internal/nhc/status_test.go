package nhc

import "testing"

func TestWorstFollowsTheBashRanking(t *testing.T) {
	// CRITICAL > WARNING > UNKNOWN > OK: UNKNOWN is deliberately NOT the worst, as in the Bash tool.
	order := []Status{OK, Unknown, Warning, Critical}
	for i, lo := range order {
		for j, hi := range order {
			want := order[max(i, j)]
			if got := Worst(lo, hi); got != want {
				t.Errorf("Worst(%s,%s)=%s want %s", lo, hi, got, want)
			}
		}
	}
}

func TestWorstOfAndExitCodes(t *testing.T) {
	cases := []struct {
		name string
		in   []Result
		want Status
		exit int
	}{
		{"empty is OK", nil, OK, 0},
		{"all ok", []Result{{Status: OK}, {Status: OK}}, OK, 0},
		{"one warning", []Result{{Status: OK}, {Status: Warning}}, Warning, 1},
		{"critical beats everything", []Result{{Status: Warning}, {Status: Critical}, {Status: Unknown}}, Critical, 2},
		{"unknown alone", []Result{{Status: OK}, {Status: Unknown}}, Unknown, 3},
		{"warning beats unknown", []Result{{Status: Unknown}, {Status: Warning}}, Warning, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WorstOf(c.in)
			if got != c.want || got.ExitCode() != c.exit {
				t.Fatalf("got %s/%d want %s/%d", got, got.ExitCode(), c.want, c.exit)
			}
		})
	}
}

func TestStatusStrings(t *testing.T) {
	for s, want := range map[Status]string{OK: "OK", Warning: "WARNING", Critical: "CRITICAL", Unknown: "UNKNOWN", Status(9): "UNKNOWN"} {
		if s.String() != want {
			t.Errorf("%d -> %q want %q", s, s.String(), want)
		}
	}
}
