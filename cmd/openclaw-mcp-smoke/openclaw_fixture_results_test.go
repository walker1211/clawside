package main

import (
	"path/filepath"
	"testing"
)

func TestBundledOpenClawSmokeFixturesAreValid(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "testdata", "openclaw-smoke", "stage0-5")

	checks := []CheckResult{
		checkOpenClawToolResults(Options{OpenClawToolResultsPath: filepath.Join(fixtureDir, "tool-results.json")}),
		checkOpenClawTruthPlaneResults(Options{OpenClawTruthPlaneResultsPath: filepath.Join(fixtureDir, "truth-plane-results.json")}),
		checkOpenClawTruthPlaneProgressionResults(Options{OpenClawTruthPlaneProgressionResultsPath: filepath.Join(fixtureDir, "progression-results.json")}),
		checkOpenClawTruthPlaneMutationResults(Options{OpenClawTruthPlaneMutationResultsPath: filepath.Join(fixtureDir, "mutation-results.json")}),
		checkOpenClawTruthPlaneRepairResults(Options{OpenClawTruthPlaneRepairResultsPath: filepath.Join(fixtureDir, "repair-results.json")}),
		checkOpenClawTruthPlaneReopenResults(Options{OpenClawTruthPlaneReopenResultsPath: filepath.Join(fixtureDir, "reopen-results.json")}),
		checkOpenClawTruthPlaneContinuityResults(Options{OpenClawTruthPlaneContinuityResultsPath: filepath.Join(fixtureDir, "continuity-results.json")}),
		checkOpenClawTruthPlaneDivergenceResults(Options{OpenClawTruthPlaneDivergenceResultsPath: filepath.Join(fixtureDir, "divergence-results.json")}),
		checkOpenClawTruthPlaneDeliveryResults(Options{OpenClawTruthPlaneDeliveryResultsPath: filepath.Join(fixtureDir, "delivery-results.json")}),
	}

	for _, check := range checks {
		if check.Status != checkStatusOK {
			t.Fatalf("expected bundled fixture check %s to pass, got %+v", check.Name, check)
		}
	}
}
