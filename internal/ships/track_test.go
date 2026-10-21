package ships

import (
	"os"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func open(mod func(*facts)) facts {
	f := facts{State: "OPEN", Base: "main", Head: "feat/x", Mergeable: "MERGEABLE", MergeState: "CLEAN",
		Created: now.Add(-time.Hour), Method: "squash",
		Checks: checks{Total: 9, Completed: 9, Started: now.Add(-50 * time.Minute), Done: now.Add(-10 * time.Minute)}}
	if mod != nil {
		mod(&f)
	}
	return f
}

func merged(runs ...run) facts {
	f := open(nil)
	f.State, f.Merged, f.Runs = "MERGED", now.Add(-20*time.Minute), runs
	return f
}

func done(workflow, conclusion string, later string) run {
	return run{Workflow: workflow, Status: "COMPLETED", Conclusion: conclusion, Created: now.Add(-19 * time.Minute), Updated: now.Add(-5 * time.Minute), Later: later}
}

// TestTrack holds every row of the note table (design 0006 §8.2).
func TestTrack(t *testing.T) {
	cases := []struct {
		name   string
		f      facts
		deploy []string
		stage  int
		kind   string
		note   string
	}{
		{"checks running", open(func(f *facts) { f.Checks.Completed, f.Checks.Started = 6, now.Add(-12*time.Minute) }), nil, StageCI, Running, "CI · 6 of 9 jobs · 12m"},
		{"a check failed", open(func(f *facts) { f.Checks.Failed, f.Checks.FailedName = 1, "e2e-web" }), nil, StageCI, Stuck, "CI failed · e2e-web"},
		{"conflicts", open(func(f *facts) { f.Mergeable, f.MergeState = "CONFLICTING", "DIRTY" }), nil, StageGreen, Stuck, "conflicts with main"},
		{"behind", open(func(f *facts) { f.MergeState = "BEHIND" }), nil, StageGreen, Stuck, "behind main"},
		{"draft", open(func(f *facts) { f.Draft = true; f.Checks.Completed = 3 }), nil, StageCI, Running, "draft"},
		{"green, not merged", open(nil), nil, StageGreen, WaitsForYou, "green · waits for your merge"},
		{"green, auto-merge on", open(func(f *facts) { f.AutoMerge = true }), nil, StageGreen, Running, "auto-merge on"},
		{"review requested", open(func(f *facts) { f.Review = "REVIEW_REQUIRED" }), nil, StageGreen, WaitsForYou, "waits for review"},
		{"changes requested", open(func(f *facts) { f.Review = "CHANGES_REQUESTED" }), nil, StageGreen, Stuck, "changes requested"},
		{"no checks, nothing blocks", open(func(f *facts) { f.Checks = checks{} }), nil, StageGreen, WaitsForYou, "green · waits for your merge"},
		{"no checks yet", open(func(f *facts) { f.Checks, f.MergeState = checks{}, "BLOCKED" }), nil, StageOpen, Running, "no checks yet"},
		{"green, a branch rule blocks it", open(func(f *facts) { f.MergeState = "BLOCKED" }), nil, StageCI, Running, "waits for a required check"},
		{"green, blocked by a review", open(func(f *facts) { f.MergeState, f.Review = "BLOCKED", "REVIEW_REQUIRED" }), nil, StageGreen, WaitsForYou, "waits for review"},
		{"green, mergeability not computed yet", open(func(f *facts) { f.Mergeable, f.MergeState = "UNKNOWN", "UNKNOWN" }), nil, StageGreen, Running, "checking mergeability"},
		{"merged, deploy running", merged(run{Workflow: "Release", Status: "IN_PROGRESS", Created: now.Add(-2 * time.Hour)}), nil, StageMerged, Running, "merged · deploying · 2h"},
		{"merged, deploy queued", merged(run{Workflow: "Release", Status: "QUEUED", Created: now.Add(-61 * time.Minute)}), nil, StageMerged, Running, "merged · deploy queued · 1h"},
		{"merged, one runs and one waits", merged(run{Workflow: "Release", Status: "PENDING", Created: now.Add(-3 * time.Hour)}, run{Workflow: "CI", Status: "IN_PROGRESS", Created: now.Add(-15 * time.Minute)}), nil, StageMerged, Running, "merged · deploying · 15m"},
		{"merged, deploy green", merged(done("Release", "SUCCESS", ""), done("CI", "SUCCESS", "")), nil, StageLive, Done, "live"},
		{"merged, deploy failed", merged(done("Release", "FAILURE", "")), nil, StageMerged, Stuck, "deploy failed · Release"},
		{"closed", func() facts { f := open(nil); f.State = "CLOSED"; return f }(), nil, StageOpen, Done, "closed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := track(c.f, c.deploy, now)
			if tr.Stage != c.stage || tr.Kind != c.kind || tr.Note != c.note {
				t.Fatalf("got %d %s %q, want %d %s %q", tr.Stage, tr.Kind, tr.Note, c.stage, c.kind, c.note)
			}
			if tr.Stages[tr.Stage].Note != c.note {
				t.Fatalf("the current stage's note is %q", tr.Stages[tr.Stage].Note)
			}
		})
	}
}

// TestLive holds design 0006 §11.9: every run on the merge commit
// finished and none failed; a cancelled or skipped run counts when a later
// run of its workflow on the default branch succeeded.
func TestLive(t *testing.T) {
	cases := []struct {
		name   string
		f      facts
		deploy []string
		kind   string
		note   string
	}{
		{"cancelled, superseded by a success", merged(done("Release", "CANCELLED", "success"), done("CI", "SUCCESS", "")), nil, Done, "live"},
		{"cancelled, the newer run still runs", merged(done("Release", "CANCELLED", "running")), nil, Running, "merged · deploying · 20m"},
		{"cancelled, the default branch not read yet", merged(done("Release", "CANCELLED", "?")), nil, Running, "merged · deploying · 20m"},
		{"failed, superseded by a success", merged(done("Release", "SUCCESS", ""), done("CI", "FAILURE", "success")), nil, Done, "live"},
		{"failed, the newer run still runs", merged(done("CI", "FAILURE", "running")), nil, Stuck, "deploy failed · CI"},
		{"failed, the default branch not read yet", merged(done("CI", "FAILURE", "?")), nil, Stuck, "deploy failed · CI"},
		{"cancelled and nothing newer", merged(done("Release", "CANCELLED", "")), nil, Stuck, "deploy cancelled · Release"},
		{"a failure outranks a cancel", merged(done("Release", "CANCELLED", ""), done("CI", "FAILURE", "")), nil, Stuck, "deploy failed · CI"},
		{"a skipped run of every run deploys nothing", merged(done("Claude Code", "SKIPPED", ""), done("Release", "SUCCESS", "")), nil, Done, "live"},
		{"a named deploy workflow that skipped waits", merged(done("Release", "SKIPPED", "")), []string{"Release"}, Stuck, "deploy skipped · Release"},
		{"a named deploy workflow skipped, then superseded", merged(done("Release", "SKIPPED", "success")), []string{"Release"}, Done, "live"},
		{"only the named workflows count", merged(done("Release", "SUCCESS", ""), done("Lint", "FAILURE", "")), []string{"Release"}, Done, "live"},
		{"no runs yet, just merged", func() facts { f := merged(); f.Merged = now.Add(-time.Minute); return f }(), nil, Running, "merged · deploying · 1m"},
		{"no runs at all", merged(), nil, Done, "live"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := track(c.f, c.deploy, now)
			if tr.Kind != c.kind || tr.Note != c.note {
				t.Fatalf("got %s %q, want %s %q", tr.Kind, tr.Note, c.kind, c.note)
			}
			if c.kind == Done && (tr.Stage != StageLive || tr.LiveAt.IsZero() || tr.Stages[StageLive].At.IsZero()) {
				t.Fatalf("live without its time: %+v", tr)
			}
		})
	}
}

// TestRecordedAnswer reads a response GitHub gave to query (names
// changed): open PRs with failed checks, conflicts, a draft behind main, a
// merged PR whose runs succeeded among queued suites of other apps, and a
// number that is no PR.
func TestRecordedAnswer(t *testing.T) {
	b, err := os.ReadFile("testdata/graphql-open-merged.json")
	if err != nil {
		t.Fatal(err)
	}
	numbers := []int{4557, 4551, 4545, 4548, 4369, 999999}
	a, err := parseAnswer(b, numbers, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Rate == nil || a.Rate.Limit != 5000 || a.Rate.Remaining != 4951 {
		t.Fatalf("rate: %+v", a.Rate)
	}
	if a.PRs[999999] != nil {
		t.Fatal("a number that is no PR has facts")
	}
	if len(a.Heads) == 0 || a.Heads["feat/4557"] != 4557 {
		t.Fatalf("open heads: %v", a.Heads)
	}
	want := map[int]struct {
		stage      int
		kind, note string
	}{
		4557: {StageCI, Stuck, "CI failed · ci-ok"},
		4551: {StageCI, Stuck, "CI failed · job"},
		4545: {StageOpen, Stuck, "conflicts with main"},
		4548: {StageCI, Stuck, "CI failed · ci-ok"},
		4369: {StageLive, Done, "live"},
	}
	for n, w := range want {
		f := a.PRs[n]
		if f == nil {
			t.Errorf("#%d: no facts", n)
			continue
		}
		if f.Method != "squash" {
			t.Errorf("#%d method %q", n, f.Method)
		}
		tr := track(*f, nil, now)
		if tr.Stage != w.stage || tr.Kind != w.kind || tr.Note != w.note {
			t.Errorf("#%d: got %d %s %q, want %d %s %q", n, tr.Stage, tr.Kind, tr.Note, w.stage, w.kind, w.note)
		}
	}
	if f := a.PRs[4557]; f.Checks.Total != 28 || f.Checks.Completed != 28 || f.Checks.Failed != 1 {
		t.Errorf("#4557 checks: %+v", f.Checks)
	}
	if f := a.PRs[4369]; len(f.Runs) != 2 || f.MergeOID == "" {
		t.Errorf("#4369 runs: only workflow runs count, got %+v", f.Runs)
	}
}

func TestLater(t *testing.T) {
	r := run{Workflow: "Release", Conclusion: "CANCELLED", Created: now}
	suite := func(name, status, conclusion string, created time.Time) suiteJSON {
		s := suiteJSON{Status: status, Conclusion: conclusion}
		s.WorkflowRun = &struct {
			CreatedAt time.Time `json:"createdAt"`
			Workflow  struct {
				Name string `json:"name"`
			} `json:"workflow"`
		}{CreatedAt: created}
		s.WorkflowRun.Workflow.Name = name
		return s
	}
	cases := []struct {
		history []suiteJSON
		want    string
	}{
		{[]suiteJSON{suite("Release", "COMPLETED", "SUCCESS", now.Add(time.Minute))}, "success"},
		{[]suiteJSON{suite("Release", "IN_PROGRESS", "", now.Add(time.Minute))}, "running"},
		{[]suiteJSON{suite("Release", "COMPLETED", "SUCCESS", now.Add(-time.Minute))}, ""}, // older
		{[]suiteJSON{suite("CI", "COMPLETED", "SUCCESS", now.Add(time.Minute))}, ""},       // another workflow
		{[]suiteJSON{{Status: "QUEUED"}}, ""},                                              // an app's suite
	}
	for i, c := range cases {
		if got := later(r, c.history); got != c.want {
			t.Errorf("%d: later = %q, want %q", i, got, c.want)
		}
	}
}

// A failed run of the merge commit asks the default branch, as a cancelled
// or skipped one does; a passed or running one does not.
func TestAFailedRunAsksTheDefaultBranch(t *testing.T) {
	for _, c := range []struct {
		r    run
		asks bool
	}{
		{run{Status: "COMPLETED", Conclusion: "FAILURE"}, true},
		{run{Status: "COMPLETED", Conclusion: "TIMED_OUT"}, true},
		{run{Status: "COMPLETED", Conclusion: "CANCELLED"}, true},
		{run{Status: "COMPLETED", Conclusion: "SKIPPED"}, true},
		{run{Status: "COMPLETED", Conclusion: "SUCCESS"}, false},
		{run{Status: "COMPLETED", Conclusion: "NEUTRAL"}, false},
		{run{Status: "IN_PROGRESS"}, false},
	} {
		f := facts{Runs: []run{c.r}}
		if got := f.superseded(); got != c.asks {
			t.Errorf("%+v: superseded %v, want %v", c.r, got, c.asks)
		}
		c.r.Later = "success"
		if f := (facts{Runs: []run{c.r}}); f.superseded() {
			t.Errorf("%+v: still waits after a later success", c.r)
		}
	}
}

func TestShipFailedName(t *testing.T) {
	red := track(open(func(f *facts) { f.Checks.Failed, f.Checks.FailedName = 1, "e2e-web" }), nil, now)
	if red.Failed != "e2e-web" {
		t.Errorf("failed %q", red.Failed)
	}
	for name, f := range map[string]facts{
		"conflicts": open(func(f *facts) { f.Mergeable = "CONFLICTING" }),
		"green":     open(nil),
	} {
		if got := track(f, nil, now).Failed; got != "" {
			t.Errorf("%s: failed %q", name, got)
		}
	}
}
