package main

import (
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	pluginapi "github.com/mattermost/mattermost-plugin-api"
	"github.com/mattermost/mattermost-server/v6/model"
	"github.com/mattermost/mattermost-server/v6/plugin"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	mm     *pluginapi.Client
	BotID  string
	router *mux.Router
}

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) OnActivate() error {
	p.mm = pluginapi.NewClient(p.API, p.Driver)
	err := p.API.RegisterCommand(&model.Command{
		Trigger:          "mpa",
		AutoComplete:     true,
		AutocompleteData: p.getAutocomplete(),
	})
	if err != nil {
		p.API.LogError(err.Error())
	}

	bot := &model.Bot{
		Username:    "mpa",
		DisplayName: "MPA",
		Description: "Multi-party Authorization bot",
	}
	botID, err := p.mm.Bot.EnsureBot(bot)
	if err != nil {
		return err
	}
	p.BotID = botID

	p.initializeAPI()

	return nil
}

// See https://developers.mattermost.com/extend/plugins/server/reference/
