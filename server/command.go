package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/pkg/errors"
)

const (
	commandTriggerAgenda = "agenda"

	wsEventList = "list"
)

// ParsedMeetingMessage is meeting message after being parsed
type ParsedMeetingMessage struct {
	date        string
	number      string
	textMessage string
}

const helpCommandText = "###### Mattermost Agenda Plugin - Slash Command Help\n" +
	"The Agenda plugin lets you queue up meeting topics for channel discussion at a later time.  When your meeting happens, you can click on the Hashtag to see all agenda items in the RHS. \n" +
	"To configure the agenda for this channel, click on the Channel Name in Mattermost to access the channel options menu and select `Agenda Settings`" +
	"\n* `/agenda queue [weekday (optional)] message` - Queue `message` as a topic on the next meeting. If `weekday` is provided, it will queue for the meeting for. \n" +
	"* `/agenda list [weekday(optional)]` - Show a list of items queued for the next meeting.  If `next-week` is provided, it will list the agenda for the next calendar week. \n" +
	"* `/agenda setting <field> <value>` - Update the setting with the given value. Field can be one of `schedule` or `hashtag` \n" +
	"How can we make this better?  Submit an issue to the [Agenda Plugin repo here](https://github.com/mattermost/mattermost-plugin-agenda/issues) \n"

func (p *Plugin) registerCommands() error {
	if err := p.API.RegisterCommand(createAgendaCommand()); err != nil {
		return errors.Wrapf(err, "failed to register %s command", commandTriggerAgenda)
	}

	return nil
}

// ExecuteCommand executes a command that has been previously registered via the RegisterCommand
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	split := strings.Fields(args.Command)

	if len(split) < 2 {
		return responsef("Missing command. You can try queue, list, setting"), nil
	}

	action := split[1]

	switch action {
	case "list":
		return p.executeCommandList(args), nil

	case "queue":
		return p.executeCommandQueue(args), nil

	case "setting":
		return p.executeCommandSetting(args), nil

	case "help":
		return p.executeCommandHelp(args), nil
	}

	return responsef("Unknown action: %s", action), nil
}

func (p *Plugin) executeCommandList(args *model.CommandArgs) *model.CommandResponse {
	split := strings.Fields(args.Command)
	nextWeek := len(split) > 2 && split[2] == "next-week"

	weekday := -1
	if !nextWeek && len(split) > 2 {
		parsedWeekday, _ := parseSchedule(split[2])
		weekday = int(parsedWeekday)
	}

	hashtag, err := p.GenerateHashtag(args.ChannelId, nextWeek, weekday)
	if err != nil {
		return responsef("Error calculating hashtags")
	}

	// Send a websocket event to the web app that will open the RHS
	p.API.PublishWebSocketEvent(
		wsEventList,
		map[string]interface{}{
			"hashtag": hashtag,
		},
		&model.WebsocketBroadcast{UserId: args.UserId},
	)

	return &model.CommandResponse{}
}

func (p *Plugin) executeCommandSetting(args *model.CommandArgs) *model.CommandResponse {
	// settings: hashtag, schedule
	split := strings.Fields(args.Command)

	if len(split) < 4 {
		return responsef("Setting parameters missing")
	}

	field := split[2]
	value := split[3]

	meeting, err := p.GetMeeting(args.ChannelId)
	if err != nil {
		return responsef("Error getting meeting information for this channel")
	}

	switch field {
	case "schedule":
		// Set schedule
		weekdayInt, err := parseSchedule(value)
		if err != nil {
			return responsef(err.Error())
		}
		meeting.Schedule = []time.Weekday{weekdayInt}

	case "hashtag":
		// Set hashtag
		meeting.HashtagFormat = value
	default:
		return responsef("Unknown setting %s", field)
	}

	if err := p.SaveMeeting(meeting); err != nil {
		return responsef("Error saving setting")
	}

	return responsef("Updated setting %v to %v", field, value)
}

func (p *Plugin) executeCommandQueue(args *model.CommandArgs) *model.CommandResponse {
	split := strings.Fields(args.Command)

	if len(split) <= 2 {
		return responsef("Missing parameters for queue command")
	}

	meeting, err := p.GetMeeting(args.ChannelId)
	if err != nil {
		p.API.LogError("failed to get meeting for channel", "err", err.Error(), "channel_id", args.ChannelId)
	}

	nextWeek := false
	weekday := -1
	message := strings.Join(split[2:], " ")

	if split[2] == "next-week" {
		nextWeek = true
	} else {
		parsedWeekday, _ := parseSchedule(split[2])
		weekday = int(parsedWeekday)
	}

	if nextWeek || weekday > -1 {
		message = strings.Join(split[3:], " ")
	}

	hashtag, err := p.GenerateHashtag(args.ChannelId, nextWeek, weekday)
	if err != nil {
		return responsef("Error calculating hashtags. Check the meeting settings for this channel.")
	}

	numQueueItems, itemErr := p.calculateQueueItemNumberAndUpdateOldItems(meeting, args, hashtag)
	if itemErr != nil {
		return responsef(itemErr.Error())
	}

	_, appErr := p.API.CreatePost(&model.Post{
		UserId:    args.UserId,
		ChannelId: args.ChannelId,
		RootId:    args.RootId,
		Message:   fmt.Sprintf("#### %v %v) %v", hashtag, numQueueItems, message),
	})
	if appErr != nil {
		return responsef("Error creating post: %s", appErr.Message)
	}

	return &model.CommandResponse{}
}

func parseMeetingPost(meeting *Meeting, post *model.Post) (string, ParsedMeetingMessage, error) {
	var (
		prefix            string
		hashtagDateFormat string
	)
	if matchGroups := meetingDateFormatRegex.FindStringSubmatch(meeting.HashtagFormat); len(matchGroups) == 4 {
		prefix = matchGroups[1]
		hashtagDateFormat = strings.TrimSpace(matchGroups[2])
	} else {
		return "", ParsedMeetingMessage{}, errors.New("error Parsing meeting post")
	}

	var (
		messageRegexFormat, err = regexp.Compile(fmt.Sprintf(`(?m)^#### #%s(?P<date>.*) ([0-9]+)\) (?P<message>.*)?$`, prefix))
	)

	if err != nil {
		return "", ParsedMeetingMessage{}, err
	}
	matchGroups := messageRegexFormat.FindStringSubmatch(post.Message)
	if len(matchGroups) == 4 {
		parsedMeetingMessage := ParsedMeetingMessage{
			date:        matchGroups[1],
			number:      matchGroups[2],
			textMessage: matchGroups[3],
		}

		return hashtagDateFormat, parsedMeetingMessage, nil
	}

	return hashtagDateFormat, ParsedMeetingMessage{}, errors.New("failed to parse meeting post's header")
}

func (p *Plugin) executeCommandHelp(args *model.CommandArgs) *model.CommandResponse {
	return responsef(helpCommandText)
}

func responsef(format string, args ...interface{}) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, args...),
		Type:         model.PostTypeDefault,
	}
}

func createAgendaCommand() *model.Command {
	agenda := model.NewAutocompleteData(commandTriggerAgenda, "[command]", "Available commands: list, queue, setting, help")

	list := model.NewAutocompleteData("list", "", "Show a list of items queued for the next meeting")
	list.AddDynamicListArgument("Day of the week for when to queue the meeting", "/api/v1/list-meeting-days-autocomplete", false)
	agenda.AddCommand(list)

	queue := model.NewAutocompleteData("queue", "", "Queue `message` as a topic on the next meeting.")
	queue.AddDynamicListArgument("Day of the week for when to queue the meeting", "/api/v1/meeting-days-autocomplete", false)
	queue.AddTextArgument("Message for the next meeting date.", "[message]", "")
	agenda.AddCommand(queue)

	setting := model.NewAutocompleteData("setting", "", "Update the setting.")
	schedule := model.NewAutocompleteData("schedule", "", "Update schedule.")
	schedule.AddStaticListArgument("weekday", true, []model.AutocompleteListItem{
		{Item: "Monday"},
		{Item: "Tuesday"},
		{Item: "Wednesday"},
		{Item: "Thursday"},
		{Item: "Friday"},
		{Item: "Saturday"},
		{Item: "Sunday"},
	})
	setting.AddCommand(schedule)
	hashtag := model.NewAutocompleteData("hashtag", "", "Update hastag.")
	hashtag.AddTextArgument("input hashtag", "Default: Jan02", "")
	setting.AddCommand(hashtag)
	agenda.AddCommand(setting)

	help := model.NewAutocompleteData("help", "", "Mattermost Agenda plugin slash command help")
	agenda.AddCommand(help)
	return &model.Command{
		Trigger:          commandTriggerAgenda,
		AutoComplete:     true,
		AutoCompleteDesc: "Available commands: list, queue, setting, help",
		AutoCompleteHint: "[command]",
		AutocompleteData: agenda,
	}
}
