package main

import (
	"context"
	"errors"
	"sort"

	"github.com/sleuth-io/sx/internal/mgmt"
)

// Bots are non-human identities skills can be installed into. Like
// repositories, they're a technical concept behind the same per-library
// opt-in (Profile.TrackRepos): people who run bots see them in the
// sidebar; everyone else never pays for the concept.

// BotInfo is the frontend view of a bot. Skills is the bot's resolved
// skill set (direct installs plus team and org-wide installs), which
// drives the sidebar count and the bot scope's asset list.
type BotInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Teams       []string `json:"teams"`
	Skills      []string `json:"skills"`
}

// BotCreated reports a new bot plus the API token Sleuth vaults auto-issue
// with it. The token is shown exactly once — file-based vaults are
// identity-only and return "".
type BotCreated struct {
	Bot   BotInfo `json:"bot"`
	Token string  `json:"token"`
}

// botManager is the slice of the vault interface bot operations need.
type botManager interface {
	ListBots(ctx context.Context) ([]mgmt.Bot, error)
	GetBot(ctx context.Context, name string) (*mgmt.Bot, error)
	CreateBot(ctx context.Context, bot mgmt.Bot) (string, error)
	UpdateBot(ctx context.Context, bot mgmt.Bot) error
	DeleteBot(ctx context.Context, name string) error
	AddBotTeam(ctx context.Context, bot, team string) error
	RemoveBotTeam(ctx context.Context, bot, team string) error
}

func (a *App) botVault() (botManager, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	bm, ok := v.(botManager)
	if !ok {
		return nil, errors.New("this library doesn't support bots")
	}
	return bm, nil
}

// ListBots returns the vault's bots with their resolved skills. Vaults
// without bot support list none rather than erroring — the sidebar
// section simply stays empty, mirroring RepoAssets.
func (a *App) ListBots() ([]BotInfo, error) {
	bm, err := a.botVault()
	if err != nil {
		return []BotInfo{}, nil
	}
	bots, err := bm.ListBots(a.ctx)
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	out := make([]BotInfo, 0, len(bots))
	for _, b := range bots {
		skills := make([]string, 0, len(b.InstalledSkills))
		for _, s := range b.InstalledSkills {
			skills = append(skills, s.Name)
		}
		sort.Strings(skills)
		out = append(out, BotInfo{
			Name:        b.Name,
			Description: b.Description,
			Teams:       append([]string{}, b.Teams...),
			Skills:      skills,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateBot makes a new bot. The returned token is non-empty only on
// Sleuth vaults (an auto-issued API key, shown once).
func (a *App) CreateBot(name, description string) (BotCreated, error) {
	name = slugify(name)
	if name == "" {
		return BotCreated{}, errors.New("give the bot a name")
	}
	bm, err := a.botVault()
	if err != nil {
		return BotCreated{}, err
	}
	token, err := bm.CreateBot(a.ctx, mgmt.Bot{Name: name, Description: description})
	if err != nil {
		return BotCreated{}, friendlyVaultError(err)
	}
	return BotCreated{
		Bot:   BotInfo{Name: name, Description: description, Teams: []string{}, Skills: []string{}},
		Token: token,
	}, nil
}

// DeleteBot removes a bot. Bot-scoped installs referencing it stop
// resolving; assets themselves are untouched. Callers confirm first.
func (a *App) DeleteBot(name string) error {
	bm, err := a.botVault()
	if err != nil {
		return err
	}
	if err := bm.DeleteBot(a.ctx, name); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// UpdateBotDescription changes a bot's description, leaving teams and
// installs untouched.
func (a *App) UpdateBotDescription(name, description string) error {
	bm, err := a.botVault()
	if err != nil {
		return err
	}
	bot, err := bm.GetBot(a.ctx, name)
	if err != nil {
		return friendlyVaultError(err)
	}
	bot.Description = description
	if err := bm.UpdateBot(a.ctx, *bot); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// SetBotTeam adds (member=true) or removes a bot from a team. Team
// membership gives the bot the team's skills and repo context.
func (a *App) SetBotTeam(bot, team string, member bool) error {
	bm, err := a.botVault()
	if err != nil {
		return err
	}
	if member {
		err = bm.AddBotTeam(a.ctx, bot, team)
	} else {
		err = bm.RemoveBotTeam(a.ctx, bot, team)
	}
	if err != nil {
		return friendlyVaultError(err)
	}
	return nil
}
