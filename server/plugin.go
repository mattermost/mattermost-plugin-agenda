package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/pkg/errors"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration

	// BotId of the created bot account.
	botID string
}

// ServeHTTP demonstrates a plugin that handles HTTP requests by greeting the world.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch path := r.URL.Path; path {
	case "/api/v1/settings":
		p.httpMeetingSettings(w, r)
	case "/api/v1/meeting-days-autocomplete":
		p.httpMeetingDaysAutocomplete(w, r, false)
	case "/api/v1/list-meeting-days-autocomplete":
		p.httpMeetingDaysAutocomplete(w, r, true)
	default:
		http.NotFound(w, r)
	}
}

// OnActivate is invoked when the plugin is activated
func (p *Plugin) OnActivate() error {
	if err := p.registerCommands(); err != nil {
		return errors.Wrap(err, "failed to register commands")
	}

	client := pluginapi.NewClient(p.API, p.Driver)
	botID, err := client.Bot.EnsureBot(&model.Bot{
		Username:    "agenda",
		DisplayName: "Agenda Plugin Bot",
		Description: "Created by the Agenda plugin.",
	})
	if err != nil {
		return errors.Wrap(err, "failed to ensure agenda bot")
	}
	p.botID = botID

	return nil
}

func (p *Plugin) httpMeetingSettings(w http.ResponseWriter, r *http.Request) {
	mattermostUserID := r.Header.Get("Mattermost-User-Id")
	if mattermostUserID == "" {
		http.Error(w, "Not Authorized", http.StatusUnauthorized)
	}

	switch r.Method {
	case http.MethodPost:
		p.httpMeetingSaveSettings(w, r, mattermostUserID)
	case http.MethodGet:
		p.httpMeetingGetSettings(w, r, mattermostUserID)
	default:
		http.Error(w, "Request: "+r.Method+" is not allowed.", http.StatusMethodNotAllowed)
	}
}

func (p *Plugin) httpMeetingSaveSettings(w http.ResponseWriter, r *http.Request, mmUserID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var meeting *Meeting
	if err = json.Unmarshal(body, &meeting); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err = p.SaveMeeting(meeting); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := struct {
		Status string
	}{"OK"}

	p.writeJSON(w, resp)
}

func (p *Plugin) httpMeetingGetSettings(w http.ResponseWriter, r *http.Request, mmUserID string) {
	channelID, ok := r.URL.Query()["channelId"]

	if !ok || len(channelID[0]) < 1 {
		http.Error(w, "Missing channelId parameter", http.StatusBadRequest)
		return
	}

	meeting, err := p.GetMeeting(channelID[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	p.writeJSON(w, meeting)
}

func (p *Plugin) writeJSON(w http.ResponseWriter, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		p.API.LogWarn("Failed to marshal JSON response", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(b)
	if err != nil {
		p.API.LogWarn("Failed to write JSON response", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (p *Plugin) httpMeetingDaysAutocomplete(w http.ResponseWriter, r *http.Request, listCommand bool) {
	query := r.URL.Query()
	meeting, err := p.GetMeeting(query.Get("channel_id"))
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting meeting days: %s", err.Error()), http.StatusInternalServerError)
		p.API.LogDebug("Failed to find meeting for autocomplete", "error", err.Error(), "listCommand", listCommand)
		return
	}

	ret := make([]model.AutocompleteListItem, 0)

	helpText := "Queue this item "
	if listCommand {
		helpText = "List items "
	}

	for _, meetingDay := range meeting.Schedule {
		ret = append(ret, model.AutocompleteListItem{
			Item:     meetingDay.String(),
			HelpText: fmt.Sprintf(helpText+"for %s's meeting", meetingDay.String()),
			Hint:     "(optional)",
		})
	}
	ret = append(ret, model.AutocompleteListItem{
		Item:     "next-week",
		HelpText: fmt.Sprintf(helpText + "for the first meeting next week"),
		Hint:     "(optional)",
	})

	jsonBytes, err := json.Marshal(ret)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error getting meeting days: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err = w.Write(jsonBytes); err != nil {
		http.Error(w, fmt.Sprintf("Error getting meeting days: %s", err.Error()), http.StatusInternalServerError)
		return
	}
}
