package main

import (
	"testing"
)

// botTestApp returns an App backed by a fresh path vault (which supports
// bot and team management) with a configured identity.
func botTestApp(t *testing.T) *App {
	t.Helper()
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	return a
}

func TestBots_CreateListDelete(t *testing.T) {
	a := botTestApp(t)

	created, err := a.CreateBot("CI Bot", "runs in CI")
	if err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if created.Bot.Name != "ci-bot" {
		t.Fatalf("name = %q, want slugified ci-bot", created.Bot.Name)
	}
	// File-based vaults are identity-only: no auto-issued token.
	if created.Token != "" {
		t.Fatalf("token = %q, want empty on a path vault", created.Token)
	}

	bots, err := a.ListBots()
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 1 || bots[0].Name != "ci-bot" || bots[0].Description != "runs in CI" {
		t.Fatalf("bots = %+v", bots)
	}
	if bots[0].Skills == nil || bots[0].Teams == nil {
		t.Fatalf("Skills/Teams must be non-nil for the frontend: %+v", bots[0])
	}

	if err := a.DeleteBot("ci-bot"); err != nil {
		t.Fatalf("DeleteBot: %v", err)
	}
	bots, err = a.ListBots()
	if err != nil {
		t.Fatalf("ListBots after delete: %v", err)
	}
	if len(bots) != 0 {
		t.Fatalf("bots after delete = %+v", bots)
	}
}

func TestBots_UpdateDescription(t *testing.T) {
	a := botTestApp(t)
	if _, err := a.CreateBot("deploy-bot", ""); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	if err := a.UpdateBotDescription("deploy-bot", "ships releases"); err != nil {
		t.Fatalf("UpdateBotDescription: %v", err)
	}
	bots, err := a.ListBots()
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 1 || bots[0].Description != "ships releases" {
		t.Fatalf("bots = %+v", bots)
	}
}

func TestBots_TeamMembership(t *testing.T) {
	a := botTestApp(t)
	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if _, err := a.CreateBot("ci-bot", ""); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	if err := a.SetBotTeam("ci-bot", "platform", true); err != nil {
		t.Fatalf("SetBotTeam add: %v", err)
	}
	bots, err := a.ListBots()
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots) != 1 || len(bots[0].Teams) != 1 || bots[0].Teams[0] != "platform" {
		t.Fatalf("bots = %+v", bots)
	}

	if err := a.SetBotTeam("ci-bot", "platform", false); err != nil {
		t.Fatalf("SetBotTeam remove: %v", err)
	}
	bots, err = a.ListBots()
	if err != nil {
		t.Fatalf("ListBots: %v", err)
	}
	if len(bots[0].Teams) != 0 {
		t.Fatalf("teams after remove = %+v", bots[0].Teams)
	}
}

func TestBots_CreateValidation(t *testing.T) {
	a := botTestApp(t)
	if _, err := a.CreateBot("   ", ""); err == nil {
		t.Fatalf("want error for empty bot name")
	}
}
