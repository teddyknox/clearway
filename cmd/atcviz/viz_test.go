package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teddyknox/clearway/internal/sim"
)

func get(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	return w
}

func TestPageAndCatalogAreServed(t *testing.T) {
	h := newMux("")
	for path, want := range map[string]string{"/": "Clearway cell viewer", "/app.js": "aircraftAt", "/style.css": "--orange"} {
		w := get(t, h, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: status %d, missing %q", path, w.Code, want)
		}
	}
	var list []Scenario
	if err := json.Unmarshal(get(t, h, "/api/scenarios").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, sc := range list {
		if sc.ID == "" || sc.Name == "" || sc.Blurb == "" || sc.Watch == "" || sc.Minutes <= 0 || seen[sc.ID] {
			t.Errorf("bad or duplicate scenario: %+v", sc)
		}
		seen[sc.ID] = true
	}
	if len(list) != len(scenarios) {
		t.Fatalf("%d scenarios served, %d defined", len(list), len(scenarios))
	}
}

// Every scenario runs through the HTTP interface and yields a recording that
// the page can replay.
func TestEveryScenarioRuns(t *testing.T) {
	h := newMux("")
	for _, sc := range scenarios {
		w := get(t, h, "/api/run?scenario="+sc.ID+"&minutes=10")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", sc.ID, w.Code, w.Body.String())
		}
		var rec sim.Recording
		if err := json.Unmarshal(w.Body.Bytes(), &rec); err != nil {
			t.Fatalf("%s: %v", sc.ID, err)
		}
		if len(rec.Frames) != 120 || rec.Meta.Duration != 600 || rec.Meta.Seed != sc.Seed {
			t.Errorf("%s: %d frames, duration %.0f, seed %d", sc.ID, len(rec.Frames), rec.Meta.Duration, rec.Meta.Seed)
		}
	}
	if w := get(t, h, "/api/run?scenario=nope"); w.Code != http.StatusNotFound {
		t.Errorf("unknown scenario: status %d", w.Code)
	}
	// The duration is clamped, and a seed can be chosen.
	var rec sim.Recording
	_ = json.Unmarshal(get(t, h, "/api/run?scenario=head-on&minutes=1&seed=7").Body.Bytes(), &rec)
	if rec.Meta.Duration != 300 || rec.Meta.Seed != 7 {
		t.Errorf("clamp and seed: duration %.0f, seed %d", rec.Meta.Duration, rec.Meta.Seed)
	}
}

type tally struct {
	kinds     map[string]int
	firstLoss float64 // minutes; -1 if none
	falseAt   float64 // minute of the first false Safety Net alert; -1 if none
	rep       *sim.Report
}

func runScenario(t *testing.T, id string) tally {
	t.Helper()
	sc := scenarioByID(id)
	if sc == nil {
		t.Fatalf("no scenario %q", id)
	}
	rec, rep, err := sim.Trace(sc.config(sc.Seed, sc.Minutes))
	if err != nil {
		t.Fatal(err)
	}
	out := tally{kinds: map[string]int{}, firstLoss: -1, falseAt: -1, rep: rep}
	for _, e := range rec.Events {
		k := e.Kind
		if k == "propose" {
			k = "rejected"
			if e.OK {
				k = "committed"
			}
		}
		out.kinds[k]++
		if e.Kind == "loss" && out.firstLoss < 0 {
			out.firstLoss = e.T / 60
		}
		if e.Kind == "alert" && e.Txt == "verified" && out.falseAt < 0 {
			out.falseAt = e.T / 60
		}
		if e.Kind == "intruder" {
			out.kinds["intruder: "+e.Txt]++
		}
	}
	return out
}

// Each scenario tells the viewer what to watch for. This test holds every
// such sentence to what the run actually does.
func TestScenariosShowTheirPoint(t *testing.T) {
	r := runScenario(t, "head-on")
	if r.kinds["committed"] < 1 || r.kinds["rejected"] < 1 || r.kinds["ack"] < 1 || r.rep.Losses() != 0 {
		t.Errorf("head-on: %v, losses %d", r.kinds, r.rep.Losses())
	}
	r = runScenario(t, "crossing")
	if r.kinds["committed"] < 4 || r.rep.Losses() != 0 || r.rep.Instructions != 0 {
		t.Errorf("crossing: %v, losses %d", r.kinds, r.rep.Losses())
	}
	r = runScenario(t, "blunder")
	if r.kinds["intruder"] < 1 || r.kinds["instruct"] < 1 || r.kinds["recontracted"] < 1 || r.rep.Losses() != 0 {
		t.Errorf("blunder: %v, losses %d", r.kinds, r.rep.Losses())
	}
	r = runScenario(t, "level-bust")
	if r.kinds["intruder: selected a wrong altitude"] != 1 || r.kinds["instruct"] != 1 || r.kinds["recontracted"] != 1 || r.rep.Losses() != 0 {
		t.Errorf("level-bust: %v, losses %d", r.kinds, r.rep.Losses())
	}
	for _, id := range []string{"normal", "dense"} {
		if r = runScenario(t, id); r.rep.Losses() != 0 || r.kinds["committed"] < 10 {
			t.Errorf("%s: %v, losses %d", id, r.kinds, r.rep.Losses())
		}
	}
	// The hostile planner: one false alert near minute 34, and no loss of the
	// kernel class with or without the lower layers.
	for _, id := range []string{"chaos", "chaos-alone"} {
		r = runScenario(t, id)
		if r.falseAt < 33 || r.falseAt > 35 || r.rep.LossKernel != 0 || r.kinds["rejected"] < 300 {
			t.Errorf("%s: false alert at minute %.1f, kernel losses %d, %v", id, r.falseAt, r.rep.LossKernel, r.kinds)
		}
	}
	if runScenario(t, "chaos-alone").rep.Instructions != 0 {
		t.Error("chaos-alone: the Safety Net instructed")
	}
	// Aircraft faults: the Safety Net's instructions make the difference.
	r = runScenario(t, "faults")
	if r.rep.Losses() != 0 || r.rep.Instructions < 2 {
		t.Errorf("faults: losses %d, instructions %d", r.rep.Losses(), r.rep.Instructions)
	}
	r = runScenario(t, "faults-alerts-only")
	if r.rep.LossIntruder != 2 || r.rep.LossKernel != 0 || r.firstLoss < 43 || r.firstLoss > 45 {
		t.Errorf("faults-alerts-only: intruder losses %d, first at minute %.1f", r.rep.LossIntruder, r.firstLoss)
	}
	r = runScenario(t, "lost-messages")
	if r.kinds["fail"] < 1 || r.kinds["settled"] < 1 || r.rep.LossKernel != 0 {
		t.Errorf("lost-messages: %v", r.kinds)
	}
	// The faulty Verifier: losses of the kernel class alone, none and alarms
	// when a correct implementation sits beside it.
	r = runScenario(t, "faulty-verifier")
	if r.rep.LossKernel < 1 {
		t.Errorf("faulty-verifier: no kernel-class loss in %v minutes", scenarioByID("faulty-verifier").Minutes)
	}
	r = runScenario(t, "faulty-verifier-pair")
	if r.rep.LossKernel != 0 || r.rep.Hard == 0 || r.kinds["alarm"] == 0 {
		t.Errorf("faulty-verifier-pair: kernel losses %d, alarms %d", r.rep.LossKernel, r.rep.Hard)
	}
}
