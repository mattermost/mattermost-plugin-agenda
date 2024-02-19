package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/pkg/errors"
)

var (
	meetingDateFormatRegex = regexp.MustCompile(`(?m)^(?P<prefix>.*)?(?:{{\s*(?P<dateformat>.*)\s*}})(?P<postfix>.*)?$`)
)

// Meeting represents a meeting agenda
type Meeting struct {
	ChannelID     string         `json:"channelId"`
	Schedule      []time.Weekday `json:"schedule"`
	HashtagFormat string         `json:"hashtagFormat"` // Default: {ChannelName}-Jan02
}

// GetMeeting returns a meeting
func (p *Plugin) GetMeeting(channelID string) (*Meeting, error) {
	meetingBytes, appErr := p.API.KVGet(channelID)
	if appErr != nil {
		return nil, appErr
	}

	var meeting *Meeting
	if meetingBytes != nil {
		if err := json.Unmarshal(meetingBytes, &meeting); err != nil {
			return nil, err
		}
	} else {
		// Return a default value
		channel, err := p.API.GetChannel(channelID)
		if err != nil {
			return nil, err
		}
		meeting = &Meeting{
			Schedule:      []time.Weekday{time.Thursday},
			HashtagFormat: strings.Join([]string{fmt.Sprintf("%.15s", channel.Name), "{{ Jan02 }}"}, "-"),
			ChannelID:     channelID,
		}
	}

	return meeting, nil
}

// SaveMeeting saves a meeting
func (p *Plugin) SaveMeeting(meeting *Meeting) error {
	jsonMeeting, err := json.Marshal(meeting)
	if err != nil {
		return err
	}

	if appErr := p.API.KVSet(meeting.ChannelID, jsonMeeting); appErr != nil {
		return appErr
	}

	return nil
}

func (p *Plugin) calculateQueueItemNumberAndUpdateOldItems(meeting *Meeting, args *model.CommandArgs, hashtag string) (int, error) {
	c, appErr := p.API.GetChannel(args.ChannelId)
	if appErr != nil {
		return 0, appErr
	}
	terms := fmt.Sprintf("in:%s %s", c.Name, hashtag)
	searchResults, appErr := p.API.SearchPostsInTeamForUser(args.TeamId, args.UserId, model.SearchParameter{Terms: &terms})
	if appErr != nil {
		return 0, errors.Wrap(appErr, "Error searching posts to find hashtags")
	}

	counter := 1

	var sortedPosts []*model.Post
	// TODO we won't need to do this once we fix https://github.com/mattermost/mattermost-server/issues/11006
	for _, post := range searchResults.PostList.Posts {
		sortedPosts = append(sortedPosts, post)
	}

	sort.Slice(sortedPosts, func(i, j int) bool {
		return sortedPosts[i].CreateAt < sortedPosts[j].CreateAt
	})

	for _, post := range sortedPosts {
		_, parsedMessage, err := parseMeetingPost(meeting, post)
		if err != nil {
			p.API.LogDebug(err.Error())
			return 0, errors.New(err.Error())
		}

		_, updateErr := p.API.UpdatePost(&model.Post{
			Id:        post.Id,
			UserId:    post.UserId,
			ChannelId: post.ChannelId,
			RootId:    post.RootId,
			Message:   fmt.Sprintf("#### %v %v) %v", hashtag, counter, parsedMessage.textMessage),
		})
		if updateErr != nil {
			return 0, errors.Wrap(updateErr, "Error updating post")
		}

		counter++
	}

	return counter, nil
}

// GenerateHashtag returns a meeting hashtag
func (p *Plugin) GenerateHashtag(channelID string, nextWeek bool, weekday int) (string, error) {
	meeting, err := p.GetMeeting(channelID)
	if err != nil {
		return "", err
	}

	var meetingDate *time.Time
	if weekday > -1 {
		// Get date for given day
		if meetingDate, err = nextWeekdayDate(time.Weekday(weekday), nextWeek); err != nil {
			return "", err
		}
	} else {
		// Get date for the list of days of the week
		if meetingDate, err = nextWeekdayDateInWeek(meeting.Schedule, nextWeek); err != nil {
			return "", err
		}
	}

	var hashtag string

	if matchGroups := meetingDateFormatRegex.FindStringSubmatch(meeting.HashtagFormat); len(matchGroups) == 4 {
		var (
			prefix        string
			hashtagFormat string
			postfix       string
		)
		prefix = matchGroups[1]
		hashtagFormat = strings.TrimSpace(matchGroups[2])
		postfix = matchGroups[3]

		hashtag = fmt.Sprintf("#%s%v%s", prefix, meetingDate.Format(hashtagFormat), postfix)
	} else {
		hashtag = fmt.Sprintf("#%s", meeting.HashtagFormat)
	}

	return hashtag, nil
}
